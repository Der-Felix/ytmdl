#!/usr/bin/env python3
"""Inventory candidate images and fail closed on any critical vulnerability."""
import argparse
import json
from pathlib import Path
import subprocess


def scan(trivy, image, component, platform, output):
    suffix = component + "-" + platform.replace("/", "-")
    report = output / ("scan-" + suffix + ".json")
    sbom = output / ("sbom-" + suffix + ".spdx.json")
    common = [trivy, "image", "--timeout", "10m", "--platform", platform,
              "--scanners", "vuln"]
    subprocess.run(common + ["--format", "json", "--output", str(report), image], check=True)
    data = json.loads(report.read_text())
    # An empty successful output must not be mistaken for a clean scan.
    if not data.get("Results") or not data.get("Metadata", {}).get("OS", {}).get("Family"):
        raise RuntimeError("Image scan did not identify an operating system and inventory.")
    vulnerabilities = [v for result in data["Results"] for v in result.get("Vulnerabilities", [])]
    critical = [v for v in vulnerabilities if v.get("Severity") == "CRITICAL"]
    subprocess.run(common + ["--format", "spdx-json", "--output", str(sbom), image], check=True)
    inventory = json.loads(sbom.read_text())
    if not inventory.get("packages") or not inventory.get("spdxVersion"):
        raise RuntimeError("Software inventory is missing packages.")
    print(component, platform, "critical=" + str(len(critical)), flush=True)
    if critical:
        raise RuntimeError("Critical image vulnerabilities block publication; inspect the saved scan report.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--trivy", default="trivy")
    parser.add_argument("--backend", required=True)
    parser.add_argument("--frontend", required=True)
    parser.add_argument("--platform", required=True, choices=("linux/amd64", "linux/arm64"))
    parser.add_argument("--output", type=Path, default=Path("dist"))
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    for component, image in (("backend", args.backend), ("frontend", args.frontend)):
        scan(args.trivy, image, component, args.platform, args.output)
