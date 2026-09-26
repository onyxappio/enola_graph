import pathlib
import tempfile
import time
import unittest
from pressure_monitor import Monitor, validate


def rows(*pairs):
    return [{"monotonic": t, "level": level} for t, level in pairs]


class PressureTests(unittest.TestCase):
    def test_coverage(self):
        self.assertEqual(validate(rows((0, 1), (.5, 1), (1, 1)), .1, .9)["samples"], 3)

    def test_rejects_bad_evidence(self):
        cases = [[], rows((0, 1)), rows((0, 1), (.5, 2), (1, 1)),
                 rows((0, 1), (.5, 4), (1, 1)), rows((0, 1), (2, 1)),
                 rows((.2, 1), (1, 1)), rows((0, 1), (.8, 1)),
                 rows((1, 1), (0, 1)), rows((0, 1), (0, 1)),
                 rows((0, 1), (float("nan"), 1)),
                 [{"monotonic": 0, "error": "unavailable"}, {"monotonic": 1, "level": 1}]]
        for case in cases:
            with self.subTest(case=case), self.assertRaises(ValueError):
                validate(case, .1, .9)

    def test_lifecycle_and_exclusive_output(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "pressure.jsonl"
            monitor = Monitor(path, lambda: 1)
            monitor.start()
            start = time.monotonic()
            end = time.monotonic()
            self.assertEqual(monitor.finish(start, end)["samples"], 2)
            with self.assertRaises(FileExistsError):
                Monitor(path, lambda: 1).start()

    def test_reader_failure_is_retained(self):
        def fail():
            raise OSError("unavailable")
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "pressure.jsonl"
            monitor = Monitor(path, fail)
            monitor.start()
            start = time.monotonic()
            end = time.monotonic()
            with self.assertRaises(ValueError):
                monitor.finish(start, end)
            self.assertIn("unavailable", path.read_text())


if __name__ == "__main__":
    unittest.main()
