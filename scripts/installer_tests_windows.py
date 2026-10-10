"""Native Windows installer contracts with real PowerShell/NTFS and harmless .NET fixtures."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
from contextlib import contextmanager
from installer_test_support import InstallerServer, codex_fixture, file_state, sha


def run(command, **kwargs):
    """Run a command with captured UTF-8 output and a timeout."""
    if Path(command[0]).name.lower() == 'powershell.exe':
        # WinPS must rebuild its own defaults, not inherit PS7 module paths.
        kwargs['env'] = {
            k: v
            for k, v in kwargs.get('env', os.environ).items()
            if k.upper() != 'PSMODULEPATH'
        }
    return subprocess.run(
        command, capture_output=True, text=True, encoding="utf-8", errors="replace",
        timeout=60, **kwargs,
    )


def succeeded(result):
    """Assert that a completed process exited successfully."""
    assert result.returncode == 0, result.stdout + result.stderr


def windows_environment(home):
    """Build an isolated environment rooted at a temporary user home."""
    system = Path(os.environ['SystemRoot'])
    # No installed user CLI or package manager can be discovered or executed.
    path = [
        system / 'System32',
        system,
        system / 'System32/WindowsPowerShell/v1.0',
        Path(shutil.which('pwsh')).parent,
    ]
    env = {
        k: v
        for k, v in os.environ.items()
        if k.upper() not in ('PATH', 'USERPROFILE', 'LOCALAPPDATA', 'CODEX_HOME')
    }
    env.update(
        PATH=';'.join(map(str, path)),
        USERPROFILE=str(home),
        LOCALAPPDATA=str(home / 'AppData/Local'),
        CODEX_HOME=str(home / 'codex-home'),
        CODEX_INSTALL_DIR=str(home / 'bin'),
        CODEX_NON_INTERACTIVE='1',
    )
    return env


@contextmanager
def preserve_user_path():
    """Restore the current user's registry Path value after the block."""
    import winreg
    with winreg.OpenKey(
        winreg.HKEY_CURRENT_USER, 'Environment', 0, winreg.KEY_READ | winreg.KEY_SET_VALUE
    ) as key:
        try:
            before = winreg.QueryValueEx(key, 'Path')
        except FileNotFoundError:
            before = None
        try:
            yield
        finally:
            if before is None:
                try:
                    winreg.DeleteValue(key, 'Path')
                except FileNotFoundError:
                    pass
            else:
                winreg.SetValueEx(key, 'Path', 0, before[1], before[0])


def compile_fixture(root, provider, version):
    """Compile a harmless .NET console fixture standing in for the official binary."""
    source = root / f'{provider}-{version}.cs'
    binary = source.with_suffix('.exe')
    version_branch = (
        'if (args.Length == 1 && args[0] == "--version") '
        f'{{ Console.WriteLine("codex-cli {version}"); return 0; }}'
        if provider == 'codex'
        else ''
    )
    source.write_text("""using System;
class Fixture {
    static int Main(string[] args) {
        @VERSION_BRANCH@
        Console.WriteLine(Environment.GetEnvironmentVariable("DISABLE_UPDATES"));
        foreach (string arg in args) Console.WriteLine(arg);
        return 23;
    }
}
""".replace('@VERSION_BRANCH@', version_branch), encoding='utf-8')
    result = run(
        [
            'powershell.exe', '-NoProfile', '-NonInteractive', '-Command',
            "$ErrorActionPreference='Stop'; "
            "Add-Type -Path $env:FIXTURE_SOURCE -OutputAssembly $env:FIXTURE_BINARY "
            "-OutputType ConsoleApplication",
        ],
        env={**os.environ, 'FIXTURE_SOURCE': str(source), 'FIXTURE_BINARY': str(binary)},
    )
    succeeded(result)
    return binary.read_bytes()


def test_windows(apps, installer_root, directory=None, requested_shell=None):
    """Run native Windows PowerShell installer contracts for every application."""
    if os.name != 'nt':
        raise RuntimeError('Native Windows required')
    engines = [requested_shell] if requested_shell else ['pwsh', 'powershell.exe']
    with tempfile.TemporaryDirectory(prefix='redapp-windows-installers-') as temp:
        root = Path(temp)
        fixtures = {}
        for app in apps:
            provider = app['installer_validator']
            for version in (['0.159.2', '0.159.3'] if provider == 'codex' else ['2.1.285']):
                if (provider, version) not in fixtures:
                    fixtures[provider, version] = compile_fixture(root, provider, version)
        for engine in engines:
            shell = shutil.which(engine)
            assert shell, f'Required Windows engine missing: {engine}'
            version = run(
                [shell, '-NoProfile', '-NonInteractive', '-Command', '$PSVersionTable.PSVersion.ToString()']
            )
            succeeded(version)
            print(f'Native Windows engine {engine}: {version.stdout.strip()}', flush=True)
            for app in apps:
                generated = directory or installer_root / app['id'] / 'generated'
                for installer in app['installers']:
                    if installer['shell'] != 'powershell':
                        continue
                    script = generated / installer['file']
                    assert script.is_file(), f'Missing exact candidate: {script}'
                    parse = (
                        "$ErrorActionPreference='Stop'; $e=$null;$t=$null;"
                        "[System.Management.Automation.Language.Parser]::ParseFile("
                        "$env:FIXTURE_SCRIPT,[ref]$t,[ref]$e)|Out-Null;"
                        "if($e.Count){Write-Output 'REDAPP_PS_PARSE_FAILED';$e;exit 1}"
                    )
                    succeeded(run(
                        [shell, '-NoProfile', '-NonInteractive', '-Command', parse],
                        env={**os.environ, 'FIXTURE_SCRIPT': str(script)},
                    ))
                provider = app['installer_validator']
                with InstallerServer(app['id'], provider) as server:
                    rendered = root / f'{provider}-{engine}.ps1'
                    rendered.write_text(
                        (generated / 'install.ps1').read_text(encoding='utf-8-sig')
                        .replace('@REDAPP_BASE_URL@', server.base),
                        encoding='utf-8-sig',
                    )
                    print(
                        f'{app["id"]}: candidate SHA256 {sha((generated / "install.ps1").read_bytes())}',
                        flush=True,
                    )
                    if provider == 'codex':
                        for legacy, target in [(False, ''), (True, 'latest'), (False, '0.159.2')]:
                            codex_case(
                                root, rendered, fixtures, shell, server,
                                legacy=legacy, requested=target, lifecycle=(target == ''),
                            )
                        for failure in [
                            'metadata', 'tag', 'missing-digest', 'missing-package',
                            'manifest-hash', 'manifest-entry', 'archive-hash', 'truncated',
                        ]:
                            codex_case(root, rendered, fixtures, shell, server, failure=failure)
                        codex_case(root, rendered, fixtures, shell, server, redirect=True)
                        print(
                            f'Codex {engine}: native x64 package/legacy, lifecycle, '
                            'integrity and stage failures PASS',
                            flush=True,
                        )
                    else:
                        body = fixtures[provider, '2.1.285']
                        claude_case(
                            root, rendered, body, shell, server.config, server.seen,
                            target='', lifecycle=True,
                        )
                        claude_case(
                            root, rendered, body, shell, server.config, server.seen,
                            arch='ARM64', platform='win32-arm64',
                        )
                        for target in ('latest', 'stable', '2.1.285'):
                            claude_case(
                                root, rendered, body, shell, server.config, server.seen,
                                target=target,
                            )
                        for failure in ['hash', 'channel', 'manifest', 'download', 'target']:
                            claude_case(
                                root, rendered, body, shell, server.config, server.seen,
                                target='../escape' if failure == 'target' else 'latest',
                                failure=failure,
                            )
                        claude_case(
                            root, rendered, body, shell, server.config, server.seen,
                            redirect=True,
                        )
                        for takeover in ['existing-launcher', 'existing-version', 'junction']:
                            claude_case(
                                root, rendered, body, shell, server.config, server.seen,
                                takeover=takeover,
                            )
                        print(
                            f'Claude {engine}: native x64 lifecycle/stage failures PASS; '
                            'ARM64 selection simulated',
                            flush=True,
                        )
        print(
            'Windows contracts PASS; no official binary executed; native ARM64/macOS not covered',
            flush=True,
        )


def claude_case(root, script, body, shell, config, seen, arch="AMD64", platform="win32-x64",
                target="latest", failure="", lifecycle=False, takeover='', redirect=False):
    """Run one Claude Code install.ps1 case and verify its filesystem effects."""
    home = root / ('user home ' + str(len(list(root.iterdir()))))
    home.mkdir()
    env = {**windows_environment(home), 'PROCESSOR_ARCHITECTURE': arch, 'DISABLE_UPDATES': '0'}
    config.clear()
    config.update(
        redirect=redirect,
        version='2.1.285',
        platform=platform,
        body=body,
        failure=failure,
        binary='claude.exe',
    )
    bin_dir = home / '.local/bin'
    versions = home / '.local/share/claude/versions'
    installed = versions / '2.1.285.exe'
    if takeover == 'existing-launcher':
        bin_dir.mkdir(parents=True)
        (bin_dir / 'claude.cmd').write_text('existing installation')
        (bin_dir / 'claude.exe').write_bytes(body)
    if takeover == 'existing-version':
        versions.mkdir(parents=True)
        installed.write_bytes(b'bad existing binary')
    sentinel = root / ('sentinel-' + home.name)
    sentinel.mkdir()
    (sentinel / 'keep.txt').write_text('do not touch')
    if takeover == 'junction':
        create = run(['cmd.exe', '/d', '/c', 'mklink', '/J', str(home / '.local'), str(sentinel)])
        assert create.returncode == 0, create.stderr
    before_bin = file_state(bin_dir)
    before_versions = file_state(versions)
    before_sentinel = file_state(sentinel)
    seen.clear()
    command = [
        shell, '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', str(script)
    ] + (['-Target', target] if target else [])
    result = run(command, env=env)
    if failure:
        assert result.returncode != 0, (failure, result.stdout, result.stderr)
        assert 'Installation complete!' not in result.stdout
        assert file_state(bin_dir) == before_bin, ('launcher changed on failure', failure)
        assert file_state(versions) == before_versions, ('version changed on failure', failure)
        assert file_state(sentinel) == before_sentinel, ('junction target changed', failure)
        expected = {
            'hash': 'Checksum verification failed',
            'channel': 'valid version',
            'manifest': 'manifest',
            'download': 'download',
            'target': 'Target',
        }
        if failure in expected:
            assert expected[failure].lower() in (result.stdout + result.stderr).lower(), result
        if failure == 'target':
            assert not seen, seen
        if failure == 'channel':
            assert seen == ['/latest'], seen
        if failure == 'manifest':
            assert not any(p.endswith('/claude.exe') for p in seen), seen
    else:
        assert result.returncode == 0, (result.stdout, result.stderr)
        assert installed.read_bytes() == body
        assert not (bin_dir / 'claude.exe').exists()
        assert ('/latest' in seen) == (target in ('', 'latest'))
        assert ('/stable' in seen) == (target == 'stable')
        assert f'/2.1.285/{platform}/claude.exe' in seen, seen
    if lifecycle:
        for entry in ('claude.ps1', 'claude.cmd'):
            # The child exit code and argument boundaries must survive each native launcher.
            invocation = (
                "& $env:FIXTURE_LAUNCHER 'argument with spaces' 'semi;colon' '--flag'; "
                "$result=$LASTEXITCODE; "
                "Write-Output ('restored=' + $env:DISABLE_UPDATES); exit $result"
            )
            launched = run(
                [
                    shell, '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass',
                    '-Command', invocation,
                ],
                env={**env, 'FIXTURE_LAUNCHER': str(bin_dir / entry)},
            )
            assert launched.returncode == 23, (entry, launched.stdout, launched.stderr)
            assert launched.stdout.splitlines() == [
                '1', 'argument with spaces', 'semi;colon', '--flag', 'restored=0'
            ], (entry, launched.stdout, launched.stderr)
        repeated = run(command, env=env)
        assert repeated.returncode == 0, (repeated.stdout, repeated.stderr)
        old = {p.name: p.read_bytes() for p in bin_dir.iterdir()}
        config['version'] = '2.1.286'
        config['failure'] = 'hash'
        upgraded = run(
            [
                shell, '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass',
                '-File', str(script), '-Target', '2.1.286',
            ],
            env=env,
        )
        assert (
            upgraded.returncode != 0
            and {p.name: p.read_bytes() for p in bin_dir.iterdir()} == old
        )
        assert not (versions / '2.1.286.exe').exists()
        config['failure'] = ''
        upgraded = run(
            [
                shell, '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass',
                '-File', str(script), '-Target', 'stable',
            ],
            env=env,
        )
        assert upgraded.returncode == 0, (upgraded.stdout, upgraded.stderr)
        assert installed.read_bytes() == body and (versions / '2.1.286.exe').read_bytes() == body
        assert all('2.1.286' in (bin_dir / name).read_text() for name in ('claude.ps1', 'claude.cmd'))
    assert not list((home / '.claude/downloads').glob('redapp-*')), (
        'temporary download directory leaked'
    )


def codex_case(root, script, fixtures, shell, server, legacy=False, requested='latest',
               failure='', lifecycle=False, redirect=False):
    """Run one Codex install.ps1 case and verify release layout and failures."""
    home = root / ('codex user home ' + str(len(list(root.iterdir()))))
    home.mkdir()
    env = windows_environment(home)
    version = '0.159.2'
    target, npm = 'x86_64-pc-windows-msvc', 'win32-x64'
    if failure == 'tag':
        requested = version

    def configure(version, failure=''):
        """Load the fixture release for version into the loopback server."""
        server.config.clear()
        server.config.update(
            codex_fixture(target, npm, legacy, version, fixtures['codex', version], failure)
        )
        server.seen.clear()

    def install(requested):
        """Run install.ps1, optionally pinning a release."""
        command = [
            shell, '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', str(script)
        ]
        return run(command + (['-Release', requested] if requested else []), env=env)

    standalone = home / 'codex-home/packages/standalone'
    releases = standalone / 'releases'
    current = standalone / 'current'
    visible = home / 'bin/codex.exe'
    marker = standalone / 'auto-update-version'
    if lifecycle:
        marker.parent.mkdir(parents=True)
        marker.write_text('old version')
    configure(version, failure)
    server.config["redirect"] = redirect
    with preserve_user_path():
        result = install(requested)
        if failure:
            assert result.returncode != 0, (failure, result.stdout, result.stderr)
            assert not current.exists() and not visible.exists(), failure
            assert (
                not list(releases.glob('*/codex.exe'))
                and not list(releases.glob('*/bin/codex.exe'))
            ), failure
            expected = {
                'metadata': 'trusted release metadata unavailable',
                'tag': 'trusted release metadata unavailable',
                'missing-digest': 'trusted release metadata unavailable',
                'missing-package': 'trusted release metadata unavailable',
                'manifest-hash': 'checksum did not match',
                'manifest-entry': 'Could not find SHA-256 digest',
                'archive-hash': 'checksum did not match',
                'truncated': 'checksum did not match',
            }
            if failure in expected:
                assert expected[failure].lower() in (result.stdout + result.stderr).lower(), result
            if failure in ('metadata', 'tag', 'missing-digest', 'missing-package'):
                assert (
                    len(server.seen) == 1
                    and server.seen[0].endswith(('latest', 'release.json'))
                ), server.seen
            if failure in ('manifest-hash', 'manifest-entry'):
                assert server.seen[-1].endswith('codex-package_SHA256SUMS'), server.seen
            if failure in ('archive-hash', 'truncated'):
                assert server.seen[-1].endswith(server.config['asset']), server.seen
        else:
            succeeded(result)
            assert (
                '/channels/latest' in server.seen
                if requested in ('', 'latest')
                else f'/releases/{version}/release.json' in server.seen
            ), server.seen
            assert visible.read_bytes() == fixtures['codex', version]
            assert current.resolve() == (releases / f'{version}-{target}').resolve()
            assert not marker.exists(), 'auto-update marker retained'
            probe = run([str(visible), '--version'], env=env)
            assert probe.returncode == 0 and probe.stdout.strip() == 'codex-cli ' + version, probe
            if lifecycle:
                succeeded(install(requested))
                before = file_state(releases)
                old_target = current.resolve()
                configure('0.159.3', 'archive-hash')
                rejected = install('0.159.3')
                assert (
                    rejected.returncode != 0
                    and 'checksum did not match' in rejected.stderr.lower()
                ), rejected
                assert file_state(releases) == before and current.resolve() == old_target, (
                    'failed upgrade changed old release'
                )
                assert visible.read_bytes() == fixtures['codex', version]
                configure('0.159.3')
                succeeded(install('0.159.3'))
                assert current.resolve() == (releases / f'0.159.3-{target}').resolve()
                assert visible.read_bytes() == fixtures['codex', '0.159.3']
                assert all(
                    (releases / name).read_bytes() == body for name, body in before.items()
                ), 'upgrade removed old release'
                assert not marker.exists()
        assert not list(releases.glob('.staging.*')), 'staging release leaked'
