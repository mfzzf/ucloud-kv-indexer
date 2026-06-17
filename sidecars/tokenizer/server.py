"""Gateway-local HuggingFace tokenizer sidecar.

The kvgateway uses this service when a remote SGLang build cannot tokenize
chat-form requests. The sidecar owns the Python-only chat-template rendering
path (`AutoTokenizer.apply_chat_template`) and returns token IDs/counts to the
Go gateway, which then applies token-count-only admission.
"""

from __future__ import annotations

import argparse
import json
import logging
import os
from pathlib import Path
from typing import Any, Literal

import uvicorn
from fastapi import FastAPI, HTTPException, Request
from pydantic import BaseModel

try:
    from .storage import (
        extract_zip_safely,
        reset_dir,
        safe_model_dir,
        sha256_text,
        write_chat_template,
        write_uploaded_file,
    )
except ImportError:  # pragma: no cover - allows `python server.py`.
    from storage import (  # type: ignore
        extract_zip_safely,
        reset_dir,
        safe_model_dir,
        sha256_text,
        write_chat_template,
        write_uploaded_file,
    )


TOKENIZER_MODE = os.environ.get("TOKENIZER_MODE", "hf").lower()
if TOKENIZER_MODE == "fastokens":
    import fastokens  # noqa: F401

    fastokens.patch_transformers()

from transformers import AutoTokenizer  # noqa: E402
import transformers  # noqa: E402


log = logging.getLogger("local-tokenizer")
logging.basicConfig(
    level=os.environ.get("LOG_LEVEL", "INFO"),
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)


class _FunctionDefinition(BaseModel):
    name: str
    description: str | None = None
    parameters: dict[str, Any] | None = None


class _ToolParam(BaseModel):
    type: Literal["function"] = "function"
    function: _FunctionDefinition


def _normalize_tools(raw: list[dict[str, Any]] | None) -> list[dict[str, Any]] | None:
    if raw is None:
        return None
    return [_ToolParam.model_validate(item).model_dump(exclude_none=False) for item in raw]


def _parse_tool_call_arguments(tool_call: Any) -> Any:
    if not isinstance(tool_call, dict):
        return tool_call
    function = tool_call.get("function")
    if not isinstance(function, dict):
        return tool_call
    arguments = function.get("arguments")
    if not isinstance(arguments, str):
        return tool_call
    try:
        parsed = json.loads(arguments) if arguments.strip() else {}
    except json.JSONDecodeError:
        return tool_call
    return {**tool_call, "function": {**function, "arguments": parsed}}


def _flatten_tool_content(content: Any) -> Any:
    if not isinstance(content, list):
        return content
    if all(
        (isinstance(p, dict) and p.get("type") == "text") or isinstance(p, str)
        for p in content
    ):
        return " ".join(p.get("text", "") if isinstance(p, dict) else p for p in content)
    return content


def _normalize_messages(messages: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Convert OpenAI wire-format quirks into the shapes chat templates expect.

    OpenAI clients send assistant tool_calls with `function.arguments` as a
    JSON string, but tool-use chat templates iterate it as a dict
    (e.g. `arguments.items()`). Tool-role content may also arrive as a list of
    text parts where templates expect a plain string.
    """
    out: list[dict[str, Any]] = []
    for message in messages:
        msg = dict(message)
        role = msg.get("role")
        if role == "tool":
            msg["content"] = _flatten_tool_content(msg.get("content"))
        elif role == "assistant" and isinstance(msg.get("tool_calls"), list):
            msg["tool_calls"] = [_parse_tool_call_arguments(tc) for tc in msg["tool_calls"]]
            if msg.get("content") is None:
                msg["content"] = ""
        out.append(msg)
    return out


class TokenizeReq(BaseModel):
    model: str
    messages: list[dict[str, Any]] | None = None
    prompt: str | None = None
    tools: list[dict[str, Any]] | None = None
    add_special_tokens: bool | None = None
    add_generation_prompt: bool | None = True
    enable_thinking: bool | None = None


class TokenizeResp(BaseModel):
    tokens: list[int]
    count: int


class ModelResp(BaseModel):
    model_id: str
    tokenizer_dir: str
    chat_template_sha256: str = ""


app = FastAPI(title="kvgateway-local-tokenizer")
TOKENIZERS: dict[str, Any] = {}
MODEL_DIRS: dict[str, str] = {}
MODEL_TEMPLATE_HASHES: dict[str, str] = {}
TOKENIZER_ROOT = Path(os.environ.get("TOKENIZER_ROOT", "/data/tokenizers"))


def _read_tokenizer_config(mdir: Path) -> dict[str, Any]:
    path = mdir / "tokenizer_config.json"
    if not path.exists():
        return {}

    try:
        parsed = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise HTTPException(status_code=400, detail=f"invalid tokenizer_config.json: {exc}") from exc

    if not isinstance(parsed, dict):
        raise HTTPException(status_code=400, detail="invalid tokenizer_config.json: expected object")
    return parsed


def _preview_bytes(path: Path, limit: int = 96) -> str:
    raw = path.read_bytes()[:limit]
    text = raw.decode("utf-8", errors="replace")
    return " ".join(text.split())


def _validate_tokenizer_files(mdir: Path, cfg: dict[str, Any]) -> None:
    if cfg.get("tokenizer_class") != "TokenizersBackend":
        return

    path = mdir / "tokenizer.json"
    if not path.exists():
        raise HTTPException(
            status_code=400,
            detail="tokenizer.json required for TokenizersBackend but was not found",
        )

    size = path.stat().st_size
    if size == 0:
        raise HTTPException(
            status_code=400,
            detail="invalid tokenizer.json: file is empty",
        )

    preview = _preview_bytes(path)
    if preview.lower().startswith(("<!doctype html", "<html")):
        raise HTTPException(
            status_code=400,
            detail=(
                "invalid tokenizer.json: file contains HTML, not JSON; "
                f"size={size}; starts_with={preview!r}"
            ),
        )

    try:
        with path.open("r", encoding="utf-8") as f:
            parsed = json.load(f)
    except json.JSONDecodeError as exc:
        raise HTTPException(
            status_code=400,
            detail=f"invalid tokenizer.json: {exc}; size={size}; starts_with={preview!r}",
        ) from exc

    if not isinstance(parsed, dict):
        raise HTTPException(
            status_code=400,
            detail=f"invalid tokenizer.json: expected object; size={size}; starts_with={preview!r}",
        )


def _load_failure_detail(exc: Exception, cfg: dict[str, Any]) -> str:
    tokenizer_class = cfg.get("tokenizer_class")
    auto_map = cfg.get("auto_map")
    detail = (
        f"load tokenizer failed: {exc}; "
        f"transformers={transformers.__version__}; "
        f"tokenizer_class={tokenizer_class!r}; auto_map={auto_map!r}"
    )
    if tokenizer_class == "TokenizersBackend":
        detail += (
            "; GLM-5.1 uses transformers' TokenizersBackend. "
            "Rebuild the tokenizer sidecar with transformers>=5.11,<6 and upload "
            "tokenizer.json plus tokenizer_config.json from the model repository."
        )
    elif tokenizer_class and not auto_map:
        detail += (
            "; tokenizer_config.json names a custom tokenizer class without auto_map. "
            "Upload the complete tokenizer repository files or use a transformers build "
            "that provides this class."
        )
    return detail


@app.get("/healthz")
def healthz() -> dict[str, Any]:
    return {
        "status": "ok",
        "mode": TOKENIZER_MODE,
        "models": sorted(TOKENIZERS.keys()),
    }


@app.get("/models")
def models() -> dict[str, Any]:
    return {
        "data": [
            {
                "model_id": model_id,
                "tokenizer_dir": MODEL_DIRS.get(model_id, ""),
                "chat_template_sha256": MODEL_TEMPLATE_HASHES.get(model_id, ""),
            }
            for model_id in sorted(TOKENIZERS)
        ]
    }


@app.post("/models", response_model=ModelResp)
async def register_model(request: Request) -> ModelResp:
    content_type = request.headers.get("content-type", "")
    if content_type.startswith("multipart/form-data"):
        payload = await _read_multipart_model(request)
    else:
        payload = await request.json()
    return _register_model(payload)


@app.post("/tokenize", response_model=TokenizeResp)
def tokenize(req: TokenizeReq) -> TokenizeResp:
    tk = TOKENIZERS.get(req.model)
    if tk is None:
        raise HTTPException(status_code=404, detail=f"unknown model: {req.model}")

    if req.messages is not None:
        messages = _normalize_messages(req.messages)
        kw: dict[str, Any] = {
            "add_generation_prompt": bool(req.add_generation_prompt),
            "tokenize": True,
        }
        if req.tools is not None:
            kw["tools"] = _normalize_tools(req.tools)
        if req.enable_thinking is not None:
            kw["enable_thinking"] = req.enable_thinking
        try:
            ids = tk.apply_chat_template(messages, **kw)
        except TypeError:
            # Older transformers may not accept tools/enable_thinking.
            kw.pop("tools", None)
            kw.pop("enable_thinking", None)
            ids = tk.apply_chat_template(messages, **kw)
        if isinstance(ids, dict) or (hasattr(ids, "get") and "input_ids" in ids):
            ids = ids["input_ids"]
        if ids and isinstance(ids[0], list):
            ids = ids[0]
    elif req.prompt is not None:
        add_special = True if req.add_special_tokens is None else bool(req.add_special_tokens)
        ids = tk.encode(req.prompt, add_special_tokens=add_special)
    else:
        raise HTTPException(status_code=400, detail="either messages or prompt required")

    return TokenizeResp(tokens=list(ids), count=len(ids))


async def _read_multipart_model(request: Request) -> dict[str, Any]:
    form = await request.form()
    model_id = str(form.get("model_id") or "")
    if not model_id:
        raise HTTPException(status_code=400, detail="model_id required")

    tokenizer_dir = str(form.get("tokenizer_dir") or "")
    chat_template = str(form.get("chat_template") or "")
    chat_template_file = form.get("chat_template_file")
    if hasattr(chat_template_file, "read"):
        raw = await chat_template_file.read()
        chat_template = raw.decode("utf-8")

    dst = safe_model_dir(TOKENIZER_ROOT, model_id)
    uploaded_any = False

    tokenizer_zip = form.get("tokenizer_zip")
    if hasattr(tokenizer_zip, "file"):
        reset_dir(dst)
        extract_zip_safely(tokenizer_zip.file, dst)
        tokenizer_dir = str(dst)
        uploaded_any = True

    for key, value in form.multi_items():
        if key not in {"tokenizer_file", "tokenizer_files"} or not hasattr(value, "file"):
            continue
        if not uploaded_any:
            reset_dir(dst)
            uploaded_any = True
        write_uploaded_file(dst, value.filename, value.file)
        tokenizer_dir = str(dst)

    return {
        "model_id": model_id,
        "tokenizer_dir": tokenizer_dir,
        "chat_template": chat_template,
    }


def _register_model(payload: dict[str, Any]) -> ModelResp:
    model_id = str(payload.get("model_id") or "")
    tokenizer_dir = str(payload.get("tokenizer_dir") or "")
    chat_template = payload.get("chat_template")
    if not model_id:
        raise HTTPException(status_code=400, detail="model_id required")
    if not tokenizer_dir:
        raise HTTPException(status_code=400, detail="tokenizer_dir or tokenizer upload required")

    mdir = Path(tokenizer_dir)
    if not mdir.exists() or not mdir.is_dir():
        raise HTTPException(status_code=400, detail=f"tokenizer_dir not found: {tokenizer_dir}")

    template_hash = ""
    if chat_template:
        try:
            template_hash = write_chat_template(mdir, str(chat_template))
        except OSError:
            # Mounted model directories may be read-only; still attach the
            # template to the live tokenizer after load.
            template_hash = sha256_text(str(chat_template))

    if (
        model_id in TOKENIZERS
        and MODEL_DIRS.get(model_id) == str(mdir)
        and (not chat_template or MODEL_TEMPLATE_HASHES.get(model_id, "") == template_hash)
    ):
        return ModelResp(
            model_id=model_id,
            tokenizer_dir=str(mdir),
            chat_template_sha256=MODEL_TEMPLATE_HASHES.get(model_id, ""),
        )

    log.info("loading tokenizer model_id=%s dir=%s", model_id, mdir)
    cfg = _read_tokenizer_config(mdir)
    _validate_tokenizer_files(mdir, cfg)
    try:
        tokenizer = AutoTokenizer.from_pretrained(str(mdir), trust_remote_code=True)
        if chat_template:
            tokenizer.chat_template = str(chat_template)
    except Exception as exc:  # noqa: BLE001 - surface transformer errors as HTTP.
        raise HTTPException(status_code=400, detail=_load_failure_detail(exc, cfg)) from exc

    TOKENIZERS[model_id] = tokenizer
    MODEL_DIRS[model_id] = str(mdir)
    MODEL_TEMPLATE_HASHES[model_id] = template_hash
    log.info("loaded tokenizer model_id=%s vocab=%s", model_id, len(tokenizer))
    return ModelResp(
        model_id=model_id,
        tokenizer_dir=str(mdir),
        chat_template_sha256=template_hash,
    )


def load_models(spec: list[dict[str, str]]) -> None:
    for entry in spec:
        _register_model(
            {
                "model_id": entry["id"],
                "tokenizer_dir": entry["dir"],
                "chat_template": entry.get("chat_template", ""),
            }
        )


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--models",
        default=os.environ.get("MODELS", ""),
        help='JSON array, e.g. [{"id":"qwen","dir":"/models/qwen"}]',
    )
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=int(os.environ.get("PORT", "9000")))
    parser.add_argument("--workers", type=int, default=int(os.environ.get("WORKERS", "1")))
    args = parser.parse_args()

    if args.models:
        spec = json.loads(args.models)
        if not isinstance(spec, list):
            raise SystemExit("--models must be a JSON array")
        load_models(spec)

    uvicorn.run(app, host=args.host, port=args.port, workers=args.workers, log_level="info")


if __name__ == "__main__":
    main()
