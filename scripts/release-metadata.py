#!/usr/bin/env python3
"""Validate release metadata; print machine-readable values or release notes."""
import argparse
import pathlib
import re
import sys


def metadata(root):
    first = (root / "debian/changelog").read_text().splitlines()[0]
    match = re.fullmatch(r"pve-snapshot-api \((\d+\.\d+\.\d+)-(\d+)\) stable; urgency=\w+", first)
    if not match:
        raise ValueError("debian/changelog must start with a stable X.Y.Z-N release")
    version, revision = match.groups()
    changelog = (root / "CHANGELOG.md").read_text()
    sections = list(re.finditer(r"^## \[(.*?)\] - (\d{4}-\d{2}-\d{2})$", changelog, re.M))
    if not sections or sections[0].group(1) != version:
        raise ValueError("latest CHANGELOG.md version must match debian/changelog")
    end = sections[1].start() if len(sections) > 1 else len(changelog)
    notes = changelog[sections[0].end():end].strip()
    if not notes:
        raise ValueError("release notes must not be empty")
    return version, f"{version}-{revision}", notes


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--notes", action="store_true")
    args = parser.parse_args()
    try:
        version, package, notes = metadata(pathlib.Path(__file__).resolve().parents[1])
    except (ValueError, OSError) as error:
        sys.exit(str(error))
    if args.notes:
        print(notes.replace("](docs/", f"](https://github.com/Freshost/pve-snapshot-api/blob/v{version}/docs/"))
    else:
        print(f"version={version}\npackage={package}\ntag=v{version}")


if __name__ == "__main__":
    main()
