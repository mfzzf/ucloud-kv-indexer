from __future__ import annotations

import hashlib
import json
import re
import shutil
import zipfile
from pathlib import Path
from typing import BinaryIO


def sha256_text(text: str) -> str:
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


def safe_model_dir(root: Path, model_id: str) -> Path:
    digest = hashlib.sha256(model_id.encode("utf-8")).hexdigest()[:16]
    slug = re.sub(r"[^A-Za-z0-9_.-]+", "-", model_id.strip()).strip("-")
    if not slug:
        slug = "model"
    return root / f"{slug[:80]}-{digest}"


def reset_dir(path: Path) -> None:
    if path.exists():
        shutil.rmtree(path)
    path.mkdir(parents=True, exist_ok=True)


def _safe_join(root: Path, name: str) -> Path:
    # Zip members and browser directory-upload filenames may contain nested
    # paths. Keep that, but reject absolute paths and '..' traversal.
    rel = Path(name)
    if rel.is_absolute() or any(part in ("", ".", "..") for part in rel.parts):
        raise ValueError(f"unsafe archive path: {name!r}")
    out = (root / rel).resolve()
    if root.resolve() not in (out, *out.parents):
        raise ValueError(f"unsafe archive path: {name!r}")
    return out


def extract_zip_safely(src: BinaryIO, dst: Path) -> None:
    dst.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(src) as zf:
        for info in zf.infolist():
            if info.is_dir():
                _safe_join(dst, info.filename).mkdir(parents=True, exist_ok=True)
                continue
            target = _safe_join(dst, info.filename)
            target.parent.mkdir(parents=True, exist_ok=True)
            with zf.open(info) as inp, target.open("wb") as out:
                shutil.copyfileobj(inp, out)


def write_uploaded_file(dst_root: Path, filename: str, src: BinaryIO) -> Path:
    target = _safe_join(dst_root, filename)
    target.parent.mkdir(parents=True, exist_ok=True)
    with target.open("wb") as out:
        shutil.copyfileobj(src, out)
    return target


def write_chat_template(tokenizer_dir: Path, chat_template: str | None) -> str:
    if not chat_template:
        return ""
    tokenizer_dir.mkdir(parents=True, exist_ok=True)
    config_path = tokenizer_dir / "tokenizer_config.json"
    data: dict[str, object] = {}
    if config_path.exists():
        data = json.loads(config_path.read_text(encoding="utf-8"))
    data["chat_template"] = chat_template
    config_path.write_text(
        json.dumps(data, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    return sha256_text(chat_template)
