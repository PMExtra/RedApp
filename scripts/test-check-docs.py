#!/usr/bin/env python3
"""Exercise the bilingual documentation check against temporary repositories."""
import importlib.util
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent


def load_check_docs():
    spec = importlib.util.spec_from_file_location('check_docs', SCRIPTS / 'check-docs.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


check_docs = load_check_docs()

ENGLISH = """# Title

Intro with a [guide](docs/guide/setup.md) and an [external link](https://example.com).

## Install

```sh
make build
```

| Key | Value |
| --- | --- |
| a | b |
"""

CHINESE = """# 标题

介绍，见[指南](docs/guide/setup.zh-CN.md)与[外部链接](https://example.com)。

## 安装

```sh
make build
```

| 键 | 值 |
| --- | --- |
| a | b |
"""


class CheckDocsTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.write('README.md', ENGLISH)
        self.write('README.zh-CN.md', CHINESE)
        self.write('docs/guide/setup.md', '# Setup\n\nBack to [README](../../README.md#title).\n')
        self.write('docs/guide/setup.zh-CN.md', '# 安装\n\n返回 [README](../../README.zh-CN.md#标题)。\n')

    def tearDown(self):
        self.temp.cleanup()

    def write(self, name, text):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding='utf-8')

    def errors(self, base=None):
        return check_docs.run(self.root, base)

    def assert_single_error(self, fragment):
        errors = self.errors()
        self.assertEqual(len(errors), 1, errors)
        self.assertIn(fragment, errors[0])

    def test_matching_pairs_pass(self):
        self.assertEqual(self.errors(), [])

    def test_missing_counterpart_is_reported(self):
        self.write('docs/guide/extra.zh-CN.md', '# 额外\n')
        self.assert_single_error('docs/guide/extra.md: missing translation counterpart')

    def test_heading_structure_must_match(self):
        self.write('README.zh-CN.md', CHINESE.replace('## 安装', '### 安装'))
        self.assert_single_error('heading levels differ')

    def test_code_blocks_must_be_identical(self):
        self.write('README.zh-CN.md', CHINESE.replace('make build', 'make check'))
        self.assert_single_error('fenced code block #1 differs')

    def test_code_block_count_must_match(self):
        self.write('README.zh-CN.md', CHINESE + '\n```sh\nmake test\n```\n')
        self.assert_single_error('fenced code block count differs')

    def test_table_rows_must_match(self):
        self.write('README.zh-CN.md', CHINESE + '| c | d |\n')
        self.assert_single_error('tables differ')

    def test_markdown_inside_code_is_not_structure(self):
        block = '\n```md\n# not a heading\n| not | table |\n[x](missing.md)\n```\n'
        self.write('README.md', ENGLISH + block)
        self.write('README.zh-CN.md', CHINESE + block)
        self.assertEqual(self.errors(), [])

    def test_broken_relative_link_is_reported_anywhere(self):
        self.write('docs/dev/notes.md', 'See [old](../archive/gone.md) and `[code](nope.md)`.\n')
        self.assert_single_error('docs/dev/notes.md:1: broken relative link ../archive/gone.md')

    def test_skipped_directories_are_ignored(self):
        self.write('third_party/README.md', '[x](missing.md)\n')
        self.write('frontend/node_modules/pkg/README.md', '[x](missing.md)\n')
        self.assertEqual(self.errors(), [])

    def test_base_requires_both_languages_to_change(self):
        self.git('init', '--quiet')
        self.git('add', '.')
        self.git('-c', 'user.name=t', '-c', 'user.email=t@example.com', 'commit', '--quiet', '-m', 'base')
        self.write('docs/guide/setup.md', '# Setup\n\nChanged.\n')
        errors = self.errors(base='HEAD')
        self.assertEqual(len(errors), 1, errors)
        self.assertIn('docs/guide/setup.md changed without docs/guide/setup.zh-CN.md', errors[0])

        self.write('docs/guide/setup.zh-CN.md', '# 安装\n\n已修改。\n')
        self.assertEqual(self.errors(base='HEAD'), [])

    def git(self, *args):
        subprocess.run(['git', *args], cwd=self.root, check=True)


if __name__ == '__main__':
    unittest.main()
