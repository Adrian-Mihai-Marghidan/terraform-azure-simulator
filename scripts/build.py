#!/usr/bin/env python3
"""Build the independent simulator using an explicit Go binary and local cache."""
import argparse
import os
import shutil
import subprocess
from pathlib import Path


def main():
    root = Path(__file__).resolve().parents[1]
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", default=shutil.which("go"))
    parser.add_argument("--output", type=Path, default=root / "bin")
    parser.add_argument("--cache", type=Path, default=root / ".tools")
    parser.add_argument("--test", action="store_true")
    args = parser.parse_args()
    if not args.go:
        parser.error("Go is a development tool; supply --go or ask the mentor to set up the simulator.")
    output, cache = args.output.resolve(), args.cache.resolve()
    output.mkdir(parents=True, exist_ok=True)
    cache.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    env.update(GOCACHE=str(cache / "gocache"), GOMODCACHE=str(cache / "gomodcache"), GOTOOLCHAIN="local")
    if args.test:
        print("Running simulator Go tests", flush=True)
        subprocess.run([args.go, "test", "-mod=readonly", "./..."], cwd=root, env=env, check=True)
    for name in ("terraform-provider-azuresim", "simctl"):
        print(f"Building {name}", flush=True)
        subprocess.run([args.go, "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-o", str(output / name), f"./cmd/{name}"],
                       cwd=root, env=env, check=True)
    print(f"Simulator binaries: {output}")


if __name__ == "__main__":
    main()
