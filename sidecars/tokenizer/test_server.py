from __future__ import annotations

import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

sys.path.insert(0, str(Path(__file__).resolve().parent))

try:
    from fastapi import HTTPException
    from server import _normalize_messages, _validate_tokenizer_files
except ImportError as exc:  # pragma: no cover - host env may not have sidecar deps.
    HTTPException = Exception  # type: ignore
    _IMPORT_ERROR = exc
else:
    _IMPORT_ERROR = None


@unittest.skipIf(_IMPORT_ERROR is not None, f"sidecar dependencies unavailable: {_IMPORT_ERROR}")
class ServerTest(unittest.TestCase):
    def test_validate_tokenizer_files_rejects_html_tokenizer_json(self) -> None:
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "tokenizer.json").write_text("<!doctype html><html></html>", encoding="utf-8")
            with self.assertRaises(HTTPException) as ctx:
                _validate_tokenizer_files(root, {"tokenizer_class": "TokenizersBackend"})
            self.assertIn("contains HTML", str(ctx.exception.detail))

    def test_normalize_messages_parses_tool_call_arguments(self) -> None:
        original = [
            {
                "role": "assistant",
                "content": None,
                "tool_calls": [
                    {
                        "id": "call_1",
                        "type": "function",
                        "function": {"name": "get_weather", "arguments": '{"city": "Beijing"}'},
                    },
                    {
                        "id": "call_2",
                        "type": "function",
                        "function": {"name": "noop", "arguments": ""},
                    },
                ],
            }
        ]
        normalized = _normalize_messages(original)
        calls = normalized[0]["tool_calls"]
        self.assertEqual(calls[0]["function"]["arguments"], {"city": "Beijing"})
        self.assertEqual(calls[1]["function"]["arguments"], {})
        self.assertEqual(normalized[0]["content"], "")
        # Original request payload must stay untouched.
        self.assertEqual(original[0]["tool_calls"][0]["function"]["arguments"], '{"city": "Beijing"}')
        self.assertIsNone(original[0]["content"])

    def test_normalize_messages_keeps_unparseable_arguments(self) -> None:
        normalized = _normalize_messages(
            [
                {
                    "role": "assistant",
                    "tool_calls": [
                        {"type": "function", "function": {"name": "f", "arguments": "not json"}}
                    ],
                }
            ]
        )
        self.assertEqual(normalized[0]["tool_calls"][0]["function"]["arguments"], "not json")

    def test_normalize_messages_flattens_tool_text_parts(self) -> None:
        normalized = _normalize_messages(
            [
                {
                    "role": "tool",
                    "tool_call_id": "call_1",
                    "content": [{"type": "text", "text": "sunny"}, "22C"],
                }
            ]
        )
        self.assertEqual(normalized[0]["content"], "sunny 22C")

    def test_normalize_messages_passes_plain_messages_through(self) -> None:
        messages = [
            {"role": "system", "content": "hi"},
            {"role": "user", "content": [{"type": "text", "text": "hello"}]},
        ]
        self.assertEqual(_normalize_messages(messages), messages)


if __name__ == "__main__":
    unittest.main()
