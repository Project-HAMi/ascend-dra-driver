# Copyright 2026 The HAMi Authors.
# SPDX-License-Identifier: Apache-2.0
"""Chart render contract tests; these do not contact Kubernetes."""

import os
import pathlib
import re
import shutil
import subprocess
import tempfile
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]
CHART = ROOT / "deployments/helm/ascend-dra-driver"


@unittest.skipUnless(shutil.which("helm"), "helm is required for render tests")
class MetricsChartTest(unittest.TestCase):
    def render(self, *settings):
        command = ["helm", "template", "ascend-dra-driver", str(CHART), "--namespace", "ascend-dra-driver"]
        for setting in settings:
            command.extend(["--set", setting])
        return subprocess.run(command, capture_output=True, text=True, check=False)

    def test_defaults_expose_individual_endpoints_without_operator(self):
        result = self.render()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("clusterIP: None", result.stdout)
        self.assertIn("--metrics-bind-address=:9395", result.stdout)
        self.assertNotIn('resources: ["pods"]', result.stdout)
        self.assertNotIn("kind: ServiceMonitor", result.stdout)

    def test_default_init_and_plugin_images_use_current_application_version(self):
        for settings in ((), ("kubeletPlugin.metrics.enabled=false",), ("kubeletPlugin.fullCardAndTraditionalVNPU.enabled=true",)):
            with self.subTest(settings=settings):
                result = self.render(*settings)
                self.assertEqual(result.returncode, 0, result.stderr)
                images = re.findall(r"^\s+image: (\S+)\s*$", result.stdout, re.MULTILINE)
                self.assertEqual(images, ["projecthami/ascend-dra-driver:v0.2.0"] * 2)
        self.assertRegex((ROOT / "common.mk").read_text(), r"(?m)^VERSION\s+\?=\s+v0\.2\.0$")

    def test_custom_image_override_is_shared_by_init_and_plugin(self):
        result = self.render("image.repository=example.test/driver", "image.tag=custom-build")
        self.assertEqual(result.returncode, 0, result.stderr)
        images = re.findall(r"^\s+image: (\S+)\s*$", result.stdout, re.MULTILINE)
        self.assertEqual(images, ["example.test/driver:custom-build"] * 2)

    def test_release_image_check_requires_a_published_default_image(self):
        with tempfile.TemporaryDirectory() as directory:
            fake_docker = pathlib.Path(directory) / "docker"
            fake_docker.write_text(
                '#!/bin/sh\nprintf \'%s\\n\' "$*" > "$CHART_IMAGE_TEST_RECORD"\nexit "$CHART_IMAGE_TEST_EXIT"\n'
            )
            fake_docker.chmod(0o755)
            record = pathlib.Path(directory) / "arguments"
            for exit_code in (0, 1):
                with self.subTest(registry_exit=exit_code):
                    env = dict(os.environ, PATH=directory + os.pathsep + os.environ["PATH"],
                               CHART_IMAGE_TEST_RECORD=str(record), CHART_IMAGE_TEST_EXIT=str(exit_code))
                    result = subprocess.run(
                        ["sh", str(ROOT / "scripts/verify-chart-image.sh"), str(CHART)],
                        env=env, capture_output=True, text=True, check=False,
                    )
                    self.assertEqual(result.returncode, exit_code, result.stderr)
                    self.assertEqual(record.read_text().strip(), "manifest inspect projecthami/ascend-dra-driver:v0.2.0")
                    if exit_code:
                        self.assertIn("Publish the matching version", result.stderr)

    def test_operator_preserves_workload_labels_and_custom_port(self):
        result = self.render("kubeletPlugin.metrics.port=19495", "kubeletPlugin.metrics.serviceMonitor.enabled=true")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("kind: ServiceMonitor", result.stdout)
        self.assertIn("containerPort: 19495", result.stdout)
        self.assertIn("port: 19495", result.stdout)
        self.assertIn("honorLabels: true", result.stdout)
        self.assertIn("targetLabel: node", result.stdout)

    def test_disabled_and_traditional_modes_do_not_expose_software_metrics(self):
        for setting in ("kubeletPlugin.metrics.enabled=false", "kubeletPlugin.fullCardAndTraditionalVNPU.enabled=true"):
            with self.subTest(setting=setting):
                result = self.render(setting, "kubeletPlugin.metrics.serviceMonitor.enabled=true")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertNotIn("kind: ServiceMonitor", result.stdout)
                self.assertNotIn("clusterIP: None", result.stdout)
                self.assertNotIn('resources: ["pods"]', result.stdout)
                self.assertIn('--metrics-bind-address=', result.stdout)

    def test_invalid_metrics_settings_are_rejected_before_install(self):
        for value in ("0", "65536", "1.5", "not-a-port"):
            with self.subTest(port=value):
                result = self.render("kubeletPlugin.metrics.port=" + value)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("port must be an integer in [1,65535]", result.stderr)
        for key in ("enabled", "serviceMonitor.enabled"):
            with self.subTest(key=key):
                result = self.render("kubeletPlugin.metrics." + key + "=not-a-boolean")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("must be a boolean", result.stderr)
        for key in ("collectionInterval", "maxAge", "serviceMonitor.interval"):
            for value in ("", "5"):
                with self.subTest(key=key, value=value):
                    result = self.render("kubeletPlugin.metrics." + key + "=" + value)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn("must be a non-empty string", result.stderr)
        for setting in ("serviceMonitor.labels=invalid", "serviceMonitor.labels.release=123"):
            with self.subTest(setting=setting):
                result = self.render("kubeletPlugin.metrics." + setting)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("labels must be a map of strings", result.stderr)


if __name__ == "__main__":
    unittest.main()
