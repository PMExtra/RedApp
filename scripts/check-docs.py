#!/usr/bin/env python3
"""Check bilingual user documentation pairs and relative Markdown links.

User-facing documents exist as an English file `X.md` and a Chinese file
`X.zh-CN.md` (the repository README and every file under docs/guide/).
Translations differ in prose, so this script compares only the structure
that must stay identical: heading levels, fenced code blocks and tables.

It also checks that relative links in every Markdown file of the repository
point to existing files. With `--base <git-ref>` it additionally requires
that a change to one file of a pair is accompanied by a change to the other.
"""
import argparse
import re
import subprocess
import sys
from pathlib import Path
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parent.parent
CHINESE_SUFFIX = '.zh-CN.md'
GUIDE_DIR = Path('docs') / 'guide'
SKIPPED_DIRS = {'node_modules', 'third_party'}

FENCE = re.compile(r'^(?P<indent> {0,3})(?P<marker>`{3,}|~{3,})')
HEADING = re.compile(r'^ {0,3}(?P<hashes>#{1,6})(\s|$)')
TABLE_ROW = re.compile(r'^\s*\|')
INLINE_CODE = re.compile(r'(`+)(?:(?!\1).)+?\1')
INLINE_LINK = re.compile(r'!?\[[^\]]*\]\(\s*(?P<target><[^>]*>|[^)\s]+)(?:\s+"[^"]*")?\s*\)')
REFERENCE_LINK = re.compile(r'^ {0,3}\[[^\]]+\]:\s*(?P<target><[^>]*>|\S+)')
URL_SCHEME = re.compile(r'^[a-zA-Z][a-zA-Z0-9+.-]*:')


class Document:
    """Structure of one Markdown file that translations must preserve."""

    def __init__(self, path):
        """Parse the structure of the Markdown file at path."""
        self.path = path
        self.headings = []
        self.code_blocks = []
        self.tables = []
        self.links = []
        self._parse(path.read_text(encoding='utf-8').splitlines())

    def _parse(self, lines):
        """Collect headings, fenced code blocks, tables and links from lines."""
        fence = None
        code = []
        table_rows = 0
        for number, line in enumerate(lines, start=1):
            if fence is not None:
                if self._closes(fence, line):
                    self.code_blocks.append('\n'.join(code))
                    fence = None
                    code = []
                else:
                    code.append(self._dedent(line, fence['indent']))
                continue

            match = FENCE.match(line)
            if match:
                table_rows = self._finish_table(table_rows)
                fence = {'indent': len(match['indent']), 'marker': match['marker']}
                continue

            if TABLE_ROW.match(line):
                table_rows += 1
            else:
                table_rows = self._finish_table(table_rows)

            heading = HEADING.match(line)
            if heading:
                self.headings.append(len(heading['hashes']))
            self._collect_links(number, line)

        self._finish_table(table_rows)
        if fence is not None:
            self.code_blocks.append('\n'.join(code))

    @staticmethod
    def _closes(fence, line):
        """Return whether line closes the open fence."""
        stripped = line.strip()
        marker = fence['marker']
        return stripped.startswith(marker) and set(stripped) == {marker[0]}

    @staticmethod
    def _dedent(line, indent):
        """Remove up to indent leading spaces from a code line."""
        removable = len(line) - len(line.lstrip(' '))
        return line[min(indent, removable):]

    def _finish_table(self, rows):
        """Record a finished table's row count and reset the counter."""
        if rows:
            self.tables.append(rows)
        return 0

    def _collect_links(self, number, line):
        """Collect inline and reference link targets outside inline code."""
        text = INLINE_CODE.sub('', line)
        for match in INLINE_LINK.finditer(text):
            self.links.append((number, match['target']))
        reference = REFERENCE_LINK.match(text)
        if reference:
            self.links.append((number, reference['target']))


def relative(path, root):
    """Return path relative to root in POSIX form."""
    return path.relative_to(root).as_posix()


def user_doc_pairs(root):
    """Return (english, chinese) relative paths for every user document pair."""
    english = {Path('README.md')}
    chinese = {Path('README' + CHINESE_SUFFIX)}
    guide = root / GUIDE_DIR
    if guide.is_dir():
        for path in guide.glob('*.md'):
            if path.name.endswith(CHINESE_SUFFIX):
                chinese.add(GUIDE_DIR / path.name)
            else:
                english.add(GUIDE_DIR / path.name)

    english_of = {path.with_name(path.name[:-len(CHINESE_SUFFIX)] + '.md'): path for path in chinese}
    names = sorted(english | set(english_of))
    return [(name, english_of.get(name, name.with_name(name.stem + CHINESE_SUFFIX))) for name in names]


def compare_pair(root, english, chinese):
    """Return error messages for one pair; both files must exist."""
    missing = [path.as_posix() for path in (english, chinese) if not (root / path).is_file()]
    if missing:
        return [f'{name}: missing translation counterpart' for name in missing]

    left = Document(root / english)
    right = Document(root / chinese)
    pair = f'{english.as_posix()} <-> {chinese.as_posix()}'
    errors = []
    if left.headings != right.headings:
        errors.append(f'{pair}: heading levels differ\n'
                      f'  {english.as_posix()}: {left.headings}\n'
                      f'  {chinese.as_posix()}: {right.headings}')
    if len(left.code_blocks) != len(right.code_blocks):
        errors.append(f'{pair}: fenced code block count differs '
                      f'({len(left.code_blocks)} vs {len(right.code_blocks)})')
    else:
        for index, (a, b) in enumerate(zip(left.code_blocks, right.code_blocks), start=1):
            if a != b:
                errors.append(f'{pair}: fenced code block #{index} differs')
    if left.tables != right.tables:
        errors.append(f'{pair}: tables differ (row counts {left.tables} vs {right.tables})')
    return errors


def markdown_files(root):
    """Yield repository Markdown files outside skipped and hidden directories."""
    for path in sorted(root.rglob('*.md')):
        parts = path.relative_to(root).parts[:-1]
        if any(part in SKIPPED_DIRS or part.startswith('.') for part in parts):
            continue
        yield path


def link_target_exists(root, source, target):
    """Return whether a relative link target resolves to an existing path."""
    if target.startswith('<') and target.endswith('>'):
        target = target[1:-1]
    if not target or target.startswith('#') or URL_SCHEME.match(target):
        return True
    path = unquote(target.split('#', 1)[0].split('?', 1)[0])
    if not path:
        return True
    resolved = root / path.lstrip('/') if path.startswith('/') else source.parent / path
    return resolved.exists()


def check_links(root):
    """Return errors for broken relative links in every Markdown file."""
    errors = []
    for path in markdown_files(root):
        for line, target in Document(path).links:
            if not link_target_exists(root, path, target):
                errors.append(f'{relative(path, root)}:{line}: broken relative link {target}')
    return errors


def changed_files(root, base):
    """Return repository-relative paths changed between base and the working tree."""
    merge_base = subprocess.run(['git', 'merge-base', base, 'HEAD'], cwd=root,
                                capture_output=True, text=True)
    reference = merge_base.stdout.strip() if merge_base.returncode == 0 else base
    result = subprocess.run(['git', 'diff', '--name-only', reference, '--'], cwd=root,
                            capture_output=True, text=True)
    if result.returncode != 0:
        raise RuntimeError(f'git diff against {base} failed: {result.stderr.strip()}')
    return {line for line in result.stdout.splitlines() if line}


def check_changed_together(pairs, changed):
    """Return errors for pairs where only one language changed."""
    errors = []
    for english, chinese in pairs:
        a, b = english.as_posix(), chinese.as_posix()
        if (a in changed) != (b in changed):
            edited, stale = (a, b) if a in changed else (b, a)
            errors.append(f'{edited} changed without {stale}; update both languages together')
    return errors


def run(root, base=None):
    """Run all documentation checks and return the error messages."""
    pairs = user_doc_pairs(root)
    errors = []
    for english, chinese in pairs:
        errors.extend(compare_pair(root, english, chinese))
    errors.extend(check_links(root))
    if base:
        errors.extend(check_changed_together(pairs, changed_files(root, base)))
    return errors


def main():
    """Parse arguments, run the checks and return the process exit code."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument('--root', type=Path, default=ROOT, help='repository root (default: this checkout)')
    parser.add_argument('--base', help='git ref; require both languages of a changed pair to change')
    args = parser.parse_args()

    try:
        errors = run(args.root.resolve(), args.base)
    except RuntimeError as error:
        print(f'check-docs: {error}', file=sys.stderr)
        return 2
    for error in errors:
        print(error, file=sys.stderr)
    if errors:
        print(f'check-docs: {len(errors)} problem(s) found', file=sys.stderr)
        return 1
    print('check-docs: OK')
    return 0


if __name__ == '__main__':
    sys.exit(main())
