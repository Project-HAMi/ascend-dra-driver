#!/usr/bin/env python3

import json
import os
import pathlib
import subprocess
import tempfile
import unittest


SCRIPTS_DIR = pathlib.Path(__file__).resolve().parent
DEMO_DIR = SCRIPTS_DIR.parent


def allocation_result(device, share_id):
    return {
        "device": device,
        "pool": "worker",
        "shareID": share_id,
        "consumedCapacity": {
            "memory": "1Gi",
            "cores": "50",
        },
    }


class DemoContractTest(unittest.TestCase):
    def test_wait_for_pod_log_pattern_retries_until_probe_is_ready(self):
        with tempfile.TemporaryDirectory() as temporary_dir:
            temporary_path = pathlib.Path(temporary_dir)
            bin_dir = temporary_path / "bin"
            bin_dir.mkdir()
            call_count = temporary_path / "kubectl-call-count"
            output_path = temporary_path / "pod.log"
            kubectl = bin_dir / "kubectl"
            kubectl.write_text(
                """#!/usr/bin/env bash
set -eu
count=0
if [[ -f \"${KUBECTL_CALL_COUNT}\" ]]; then
  count=$(<\"${KUBECTL_CALL_COUNT}\")
fi
count=$((count + 1))
printf '%s\\n' \"${count}\" > \"${KUBECTL_CALL_COUNT}\"
if [[ \"${count}\" -eq 1 ]]; then
  printf '%s\\n' 'Initialize SchedulerClient...'
else
  printf '%s\\n' 'set_device_ret=0 probe_ret=0 free=1 total=1073741824'
fi
""",
                encoding="utf-8",
            )
            kubectl.chmod(0o755)
            env = os.environ.copy()
            env["PATH"] = f"{bin_dir}:{env['PATH']}"
            env["KUBECTL_CALL_COUNT"] = str(call_count)
            result = subprocess.run(
                [
                    "bash",
                    "-c",
                    f"source '{SCRIPTS_DIR / 'common.sh'}'; "
                    "wait_for_pod_log_pattern test-ns test-pod "
                    "'set_device_ret=0.*total=1073741824' "
                    f"'{output_path}' 3 0",
                ],
                check=False,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                universal_newlines=True,
                env=env,
            )

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(call_count.read_text(encoding="utf-8").strip(), "2")
            self.assertIn("set_device_ret=0", result.stdout)
            self.assertIn(
                "total=1073741824", output_path.read_text(encoding="utf-8")
            )

    def test_claim_a_uses_one_device_and_claim_b_uses_two(self):
        claims = {
            "items": [
                {
                    "metadata": {"name": "npu-share-a"},
                    "status": {
                        "allocation": {
                            "devices": {
                                "results": [allocation_result("npu-4-0", "share-a")]
                            }
                        }
                    },
                },
                {
                    "metadata": {"name": "npu-share-b"},
                    "status": {
                        "allocation": {
                            "devices": {
                                "results": [
                                    allocation_result("npu-4-0", "share-b-0"),
                                    allocation_result("npu-9-0", "share-b-1"),
                                ]
                            }
                        }
                    },
                },
            ]
        }
        with tempfile.TemporaryDirectory() as temporary_dir:
            claims_path = pathlib.Path(temporary_dir) / "claims.json"
            claims_path.write_text(json.dumps(claims), encoding="utf-8")
            result = subprocess.run(
                [str(SCRIPTS_DIR / "assert-claims.py"), str(claims_path)],
                check=False,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                universal_newlines=True,
            )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("claim_a_device_count=1", result.stdout)
        self.assertIn("claim_b_device_count=2", result.stdout)

    def test_manifest_requests_two_devices_for_claim_b(self):
        template = (DEMO_DIR / "templates" / "workloads.yaml.tpl").read_text(
            encoding="utf-8"
        )
        claim_b = template.split("name: npu-share-b", 1)[1].split("---", 1)[0]
        self.assertIn("count: 2", claim_b)
        self.assertIn("constraints:", claim_b)
        self.assertIn("distinctAttribute: ascend.project-hami.io/index", claim_b)
        self.assertIn("memory: 1Gi", claim_b)
        self.assertIn("cores: 50", claim_b)
        self.assertNotIn("/aicore", claim_b)

        setup_template = (
            DEMO_DIR / "templates" / "setup-resources.yaml.tpl"
        ).read_text(encoding="utf-8")
        self.assertNotIn(".index == 0", setup_template)
        self.assertIn('device.driver == "ascend.project-hami.io"', setup_template)
        self.assertIn('.type == "HAMivNPUCore"', setup_template)
        self.assertNotIn('.type == "NPU"', setup_template)


if __name__ == "__main__":
    unittest.main()
