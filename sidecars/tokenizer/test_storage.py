from __future__ import annotations

import io
import json
import sys
import unittest
import zipfile
from pathlib import Path
from tempfile import TemporaryDirectory

sys.path.insert(0, str(Path(__file__).resolve().parent))

from storage import (
    extract_zip_safely,
    safe_model_dir,
    sha256_text,
    write_chat_template,
)


class StorageTest(unittest.TestCase):
    def test_safe_model_dir_is_stable_and_under_root(self) -> None:
        root = Path("/tmp/tokenizers")
        a = safe_model_dir(root, "/mnt/models/deepseek-ai/DeepSeek-V4-Pro")
        b = safe_model_dir(root, "/mnt/models/deepseek-ai/DeepSeek-V4-Pro")
        self.assertEqual(a, b)
        self.assertIn(root, a.parents)
        self.assertNotIn("/", a.name)

    def test_extract_zip_rejects_path_traversal(self) -> None:
        buf = io.BytesIO()
        with zipfile.ZipFile(buf, "w") as zf:
            zf.writestr("../escape.txt", "bad")
        buf.seek(0)
        with TemporaryDirectory() as tmp:
            with self.assertRaises(ValueError):
                extract_zip_safely(buf, Path(tmp))

    def test_write_chat_template_updates_tokenizer_config(self) -> None:
        template = "{% for message in messages %}{{ message['content'] }}{% endfor %}"
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "tokenizer_config.json").write_text(
                json.dumps({"model_max_length": 128}),
                encoding="utf-8",
            )
            digest = write_chat_template(root, template)
            self.assertEqual(digest, sha256_text(template))
            config = json.loads((root / "tokenizer_config.json").read_text(encoding="utf-8"))
            self.assertEqual(config["chat_template"], template)
            self.assertEqual(config["model_max_length"], 128)


if __name__ == "__main__":
    unittest.main()
