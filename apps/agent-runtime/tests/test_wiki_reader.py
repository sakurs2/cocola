"""Resource-boundary tests for the sandbox Office Wiki reader."""

import importlib.util
import sys
import types
from pathlib import Path

import pytest

WIKI_READER_PATH = (
    Path(__file__).resolve().parents[3] / "deploy" / "sandbox-runtime" / "wiki_reader.py"
)


def _load_wiki_reader():
    spec = importlib.util.spec_from_file_location("cocola_wiki_reader", WIKI_READER_PATH)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_xlsx_range_rejects_excessive_cell_count_before_iteration(monkeypatch):
    reader = _load_wiki_reader()

    class Sheet:
        def iter_rows(self, **_kwargs):
            raise AssertionError("iter_rows must not run for an excessive range")

    class Workbook:
        sheetnames = ["Sheet1"]

        def __getitem__(self, _name):
            return Sheet()

    openpyxl = types.ModuleType("openpyxl")
    openpyxl.load_workbook = lambda *_args, **_kwargs: Workbook()
    openpyxl_utils = types.ModuleType("openpyxl.utils")
    openpyxl_cell = types.ModuleType("openpyxl.utils.cell")
    openpyxl_cell.range_boundaries = lambda _value: (1, 1, 16384, 1048576)
    monkeypatch.setitem(sys.modules, "openpyxl", openpyxl)
    monkeypatch.setitem(sys.modules, "openpyxl.utils", openpyxl_utils)
    monkeypatch.setitem(sys.modules, "openpyxl.utils.cell", openpyxl_cell)

    with pytest.raises(SystemExit, match="range exceeds"):
        reader._xlsx_range(Path("large.xlsx"), "Sheet1", "A1:XFD1048576", "formula")


def test_docx_output_is_truncated_at_the_reader_boundary(monkeypatch, capsys):
    reader = _load_wiki_reader()
    paragraph = types.SimpleNamespace(
        text="x" * 1024,
        style=types.SimpleNamespace(name="Normal"),
    )
    document = types.SimpleNamespace(paragraphs=[paragraph] * 1500, tables=[])
    docx = types.ModuleType("docx")
    docx.Document = lambda _path: document
    monkeypatch.setitem(sys.modules, "docx", docx)
    monkeypatch.setattr(reader, "_validate_office_archive", lambda _path: None)
    monkeypatch.setattr(Path, "is_file", lambda _path: True)
    monkeypatch.setattr(sys, "argv", ["wiki_reader.py", "docx", "large.docx"])

    reader.main()

    output = capsys.readouterr().out
    assert "[output truncated" in output
    assert len(output.encode("utf-8")) <= (1 << 20) + 128


def test_office_archive_rejects_excessive_declared_expansion(monkeypatch):
    reader = _load_wiki_reader()
    entry = types.SimpleNamespace(
        file_size=reader.MAX_OFFICE_EXPANDED_BYTES + 1,
        is_dir=lambda: False,
    )

    class Archive:
        def __enter__(self):
            return self

        def __exit__(self, *_args):
            return False

        def infolist(self):
            return [entry]

    monkeypatch.setattr(reader.zipfile, "ZipFile", lambda _path: Archive())

    with pytest.raises(SystemExit, match="expands beyond"):
        reader._validate_office_archive(Path("large.docx"))
