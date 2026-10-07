import importlib.util
import io
from pathlib import Path
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "calc_test_groups", Path(__file__).with_name("calc-test-groups.py")
)
groups = importlib.util.module_from_spec(spec)
spec.loader.exec_module(groups)

blackbox_spec = importlib.util.spec_from_file_location(
    "calc_blackbox_groups", Path(__file__).with_name("calc-blackbox-groups.py")
)
blackbox = importlib.util.module_from_spec(blackbox_spec)
blackbox_spec.loader.exec_module(blackbox)


class TestUntimedPackages(unittest.TestCase):
    def test_new_packages_spread_across_non_primary_runners(self):
        runners, totals = groups.pack_lpt(
            [{"package": "slow", "elapsed_s": 12}], 3, 2,
            new_packages=["new-a", "new-b", "new-c"],
        )
        output = groups.build_output(runners, totals, 2)
        self.assertEqual(
            [g["packages"] for g in output["groups"]],
            [["slow"], ["new-a", "new-c"], ["new-b"]],
        )
        self.assertEqual([g["estimated_s"] for g in output["groups"]],
                         [12, 2.02, 2.01])

    def test_single_runner_keeps_new_packages(self):
        runners, totals = groups.pack_lpt([], 1, 2, new_packages=["new-a"])
        output = groups.build_output(runners, totals, 2)
        self.assertEqual(output["groups"][0]["packages"], ["new-a"])
        self.assertEqual(output["makespan_s"], 2.01)


class TestBlackboxDiscovery(unittest.TestCase):
    def test_untimed_tests_respect_environment(self):
        for environment, expected in [
            ("standalone", ["TestNew"]),
            ("peers", ["TestDistributedNew", "TestNew", "TestMeasuredStandalone"]),
        ]:
            with self.subTest(environment=environment), \
                    patch.object(blackbox, "load_times", return_value=[{
                        "name": "TestMeasuredStandalone", "environment": "standalone", "elapsed_s": 7,
                    }]), \
                    patch.object(blackbox, "discover_tests", return_value=[
                        "TestDistributedNew", "TestNew", "TestMeasuredStandalone", "TestPOP",
                    ]), \
                    patch.object(blackbox, "pack_lpt", wraps=blackbox.pack_lpt) as pack, \
                    patch("sys.argv", ["calc-blackbox-groups.py", "times.json", "--env", environment]), \
                    patch("sys.stdout", new_callable=io.StringIO):
                blackbox.main()
                self.assertEqual(pack.call_args.kwargs["new_tests"], expected)


if __name__ == "__main__":
    unittest.main()
