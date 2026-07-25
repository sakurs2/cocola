#!/opt/cocola/venv/bin/python
"""Read-only Office helpers for Cocola Wiki files materialized in a sandbox."""

from __future__ import annotations

import argparse
import csv
import json
import sys
from pathlib import Path


def _docx(path: Path) -> None:
    from docx import Document

    document = Document(path)
    for paragraph in document.paragraphs:
        text = paragraph.text.strip()
        if not text:
            continue
        style = (paragraph.style.name if paragraph.style else "").lower()
        if style.startswith("heading"):
            level = "".join(char for char in style if char.isdigit()) or "1"
            print(f"{'#' * min(int(level), 6)} {text}")
        else:
            print(text)
        print()
    for table_index, table in enumerate(document.tables, 1):
        print(f"## Table {table_index}")
        rows = [[cell.text.replace("\n", " ").strip() for cell in row.cells] for row in table.rows]
        if not rows:
            continue
        print("| " + " | ".join(rows[0]) + " |")
        print("| " + " | ".join("---" for _ in rows[0]) + " |")
        for row in rows[1:]:
            print("| " + " | ".join(row) + " |")
        print()


def _pptx(path: Path) -> None:
    from pptx import Presentation

    presentation = Presentation(path)
    for index, slide in enumerate(presentation.slides, 1):
        print(f"# Slide {index}")
        for shape in slide.shapes:
            if getattr(shape, "has_text_frame", False):
                text = shape.text.strip()
                if text:
                    print(text)
                    print()
            if getattr(shape, "has_table", False):
                rows = [
                    [cell.text.replace("\n", " ").strip() for cell in row.cells]
                    for row in shape.table.rows
                ]
                if rows:
                    print("| " + " | ".join(rows[0]) + " |")
                    print("| " + " | ".join("---" for _ in rows[0]) + " |")
                    for row in rows[1:]:
                        print("| " + " | ".join(row) + " |")
                    print()
        notes_slide = getattr(slide, "notes_slide", None)
        notes_frame = getattr(notes_slide, "notes_text_frame", None)
        notes = getattr(notes_frame, "text", "").strip()
        if notes:
            print("## Speaker notes")
            print(notes)
            print()


def _xlsx_info(path: Path) -> None:
    from openpyxl import load_workbook

    workbook = load_workbook(path, read_only=True, data_only=False)
    result = {
        "sheets": [
            {
                "name": sheet.title,
                "max_row": sheet.max_row,
                "max_column": sheet.max_column,
            }
            for sheet in workbook.worksheets
        ]
    }
    print(json.dumps(result, ensure_ascii=False, indent=2))


def _xlsx_range(path: Path, sheet_name: str, cell_range: str, mode: str) -> None:
    from openpyxl import load_workbook
    from openpyxl.utils.cell import range_boundaries

    workbook = load_workbook(path, read_only=True, data_only=mode == "cached")
    if sheet_name not in workbook.sheetnames:
        raise SystemExit(f"sheet not found: {sheet_name}")
    min_column, min_row, max_column, max_row = range_boundaries(cell_range)
    sheet = workbook[sheet_name]
    writer = csv.writer(sys.stdout)
    for row in sheet.iter_rows(
        min_row=min_row,
        max_row=max_row,
        min_col=min_column,
        max_col=max_column,
        values_only=True,
    ):
        writer.writerow(["" if value is None else value for value in row])


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Read DOCX, XLSX, and PPTX Wiki files without modifying them."
    )
    commands = parser.add_subparsers(dest="command", required=True)
    for command in ("docx", "pptx", "xlsx-info"):
        command_parser = commands.add_parser(command)
        command_parser.add_argument("path", type=Path)
    range_parser = commands.add_parser("xlsx-range")
    range_parser.add_argument("path", type=Path)
    range_parser.add_argument("--sheet", required=True)
    range_parser.add_argument("--range", dest="cell_range", required=True)
    range_parser.add_argument("--mode", choices=("formula", "cached"), default="formula")
    args = parser.parse_args()
    if not args.path.is_file():
        raise SystemExit(f"file not found: {args.path}")
    if args.command == "docx":
        _docx(args.path)
    elif args.command == "pptx":
        _pptx(args.path)
    elif args.command == "xlsx-info":
        _xlsx_info(args.path)
    else:
        _xlsx_range(args.path, args.sheet, args.cell_range, args.mode)


if __name__ == "__main__":
    main()
