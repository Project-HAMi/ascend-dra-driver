#!/usr/bin/env python3

import json
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
            "npu.project-hami.io/memory": "1Gi",
            "npu.project-hami.io/aicore": "50",
        },
    }


class DemoContractTest(unittest.TestCase):
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
        self.assertIn("distinctAttribute: npu.project-hami.io/index", claim_b)

        setup_template = (
            DEMO_DIR / "templates" / "setup-resources.yaml.tpl"
        ).read_text(encoding="utf-8")
        self.assertNotIn(".index == 0", setup_template)


if __name__ == "__main__":
    unittest.main()
