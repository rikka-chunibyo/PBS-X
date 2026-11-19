#!/usr/bin/env python3
"""Extract portable Windows client sources from an immutable GitHub commit ZIP."""
import argparse
import os
from pathlib import Path, PurePosixPath
import re
import stat
import tempfile
import urllib.parse
import urllib.request
import zipfile

FILES = {'.gitignore', 'tools/build-host-app.py'}
TREES = ('pbsx-host-agent/', 'packaging/host-app/')
LIMIT = 128 * 1024 * 1024


class ArchiveRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        parsed = urllib.parse.urlsplit(newurl)
        if parsed.scheme != 'https' or parsed.hostname not in {'api.github.com', 'codeload.github.com'}:
            raise ValueError('unexpected archive redirect')
        redirected = super().redirect_request(req, fp, code, msg, headers, newurl)
        if redirected is not None:
            redirected.remove_header('Authorization')
        return redirected


def extract(archive_path, output, sha):
    if not re.fullmatch(r'[0-9a-f]{40}', sha):
        raise ValueError('expected an exact commit SHA')
    output = Path(output)
    with zipfile.ZipFile(archive_path) as archive:
        if archive.comment.decode('ascii', errors='strict').strip() != sha:
            raise ValueError('GitHub archive commit does not match requested SHA')
        selected, seen, total = [], set(), 0
        roots = set()
        for info in archive.infolist():
            raw = info.filename
            parts = PurePosixPath(raw).parts
            if not parts:
                continue
            roots.add(parts[0])
            if len(roots) > 1:
                raise ValueError('multiple archive roots')
            relative = '/'.join(parts[1:])
            if relative not in FILES and not relative.startswith(TREES):
                continue
            if '\\' in raw or raw.startswith('/') or any(part in {'.', '..'} for part in raw.split('/')):
                raise ValueError('unsafe portable source path')
            for part in parts[1:]:
                if (re.search(r'[<>:"|?*\x00-\x1f]', part) or part.endswith((' ', '.'))
                        or part.split('.')[0].upper() in {'CON', 'PRN', 'AUX', 'NUL',
                            *('COM'+str(i) for i in range(1, 10)), *('LPT'+str(i) for i in range(1, 10))}):
                    raise ValueError('nonportable source filename')
            mode = info.external_attr >> 16
            if stat.S_ISLNK(mode):
                raise ValueError('source archive symlink')
            if info.is_dir():
                continue
            key = relative.casefold()
            if key in seen:
                raise ValueError('duplicate portable source path')
            seen.add(key)
            total += info.file_size
            if info.file_size > 16 * 1024 * 1024 or total > LIMIT:
                raise ValueError('source archive exceeds limit')
            selected.append((info, relative))
        required = {'.gitignore', 'tools/build-host-app.py', 'pbsx-host-agent/go.mod',
                    'pbsx-host-agent/third_party/pxar/go.mod'}
        if not required.issubset({relative for _, relative in selected}):
            raise ValueError('portable client sources are incomplete')
        # Check all destinations before writing; a reused checkout must not be overwritten.
        for _, relative in selected:
            target = output / relative
            if target.exists() or target.is_symlink():
                raise ValueError('source destination already exists')
            parent = target.parent
            while parent != output and parent != parent.parent:
                if parent.is_symlink():
                    raise ValueError('source destination contains symlink')
                parent = parent.parent
        if (output / '.repoid').exists() or (output / '.repoid').is_symlink():
            raise ValueError('source identity already exists')
        for info, relative in selected:
            target = output / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            with archive.open(info) as source, target.open('xb') as dest:
                copied = 0
                while data := source.read(65536):
                    copied += len(data)
                    if copied > info.file_size:
                        raise ValueError('source file exceeds announced size')
                    dest.write(data)
        (output / '.repoid').write_text(sha+'\n', encoding='ascii')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repository', required=True)
    parser.add_argument('--sha', required=True)
    parser.add_argument('--output', type=Path, default=Path('.'))
    args = parser.parse_args()
    if not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', args.repository) or not re.fullmatch(r'[a-f0-9]{40}', args.sha):
        raise ValueError('expected repository and exact commit SHA')
    headers = {'User-Agent': 'PBS-X portable client CI', 'Accept': 'application/vnd.github+json'}
    if token := os.environ.get('GH_TOKEN'):
        headers['Authorization'] = 'Bearer '+token
    request = urllib.request.Request('https://api.github.com/repos/'+args.repository+'/zipball/'+args.sha, headers=headers)
    opener = urllib.request.build_opener(ArchiveRedirects())
    with tempfile.TemporaryDirectory(prefix='pbsx-source-') as work:
        filename = Path(work)/'source.zip'
        with opener.open(request, timeout=60) as response, filename.open('xb') as dest:
            total = 0
            while chunk := response.read(65536):
                total += len(chunk)
                if total > LIMIT:
                    raise ValueError('source download exceeds limit')
                dest.write(chunk)
        extract(filename, args.output.resolve(), args.sha)
    print('Extracted portable sources for', args.sha)


if __name__ == '__main__':
    main()
