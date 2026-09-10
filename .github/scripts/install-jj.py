"""Install a pinned jj release into a job-local directory for integration tests."""

import io
import pathlib
import platform
import sys
import tarfile
import urllib.request

version, directory = sys.argv[1:]
architecture = {"arm64": "aarch64", "aarch64": "aarch64", "x86_64": "x86_64"}[
    platform.machine()
]
target = {"Darwin": "apple-darwin", "Linux": "unknown-linux-musl"}[platform.system()]
archive = f"jj-v{version}-{architecture}-{target}.tar.gz"
url = f"https://github.com/jj-vcs/jj/releases/download/v{version}/{archive}"
with urllib.request.urlopen(url, timeout=60) as response:
    data = response.read()
with tarfile.open(fileobj=io.BytesIO(data), mode="r:gz") as release:
    matches = [
        member
        for member in release
        if member.isfile() and pathlib.PurePosixPath(member.name).name == "jj"
    ]
    if len(matches) != 1:
        raise RuntimeError("expected exactly one jj binary in the release archive")
    binary = pathlib.Path(directory) / "jj"
    binary.parent.mkdir(parents=True, exist_ok=True)
    binary.write_bytes(release.extractfile(matches[0]).read())
    binary.chmod(0o755)
print(binary)
