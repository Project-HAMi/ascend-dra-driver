#!/usr/bin/env python3

"""Render @@NAME@@ placeholders without interpreting shell or YAML syntax."""

import pathlib
import sys


def main():
    if len(sys.argv) < 3 or (len(sys.argv) - 3) % 2:
        print(
            "usage: render-template.py TEMPLATE OUTPUT [NAME VALUE ...]",
            file=sys.stderr,
        )
        return 2

    template = pathlib.Path(sys.argv[1])
    output = pathlib.Path(sys.argv[2])
    content = template.read_text(encoding="utf-8")

    arguments = iter(sys.argv[3:])
    for name, value in zip(arguments, arguments):
        content = content.replace(f"@@{name}@@", value)

    unresolved = sorted(
        {
            fragment.split("@@", 1)[0]
            for fragment in content.split("@@")[1::2]
            if fragment
        }
    )
    if unresolved:
        print(
            f"unresolved placeholders in {template}: {', '.join(unresolved)}",
            file=sys.stderr,
        )
        return 1

    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(content, encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
