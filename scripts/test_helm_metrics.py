# Copyright 2026 The HAMi Authors.
# SPDX-License-Identifier: Apache-2.0
"""Chart render contract tests; these do not contact Kubernetes."""

import pathlib
import shutil
import subprocess
import unittest


CHART = pathlib.Path(__file__).resolve().parents[1] / "deployments/helm/ascend-dra-driver"


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
