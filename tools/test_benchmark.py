"""Isolated tests for the benchmark's consumer/oracle, never a mocked MSS CLI."""

import copy
from pathlib import Path
import tempfile
import unittest

import benchmark


def envelope(match="found", rows=None):
    rows = [] if rows is None else rows
    return {"schema_version": 2, "produced_by": "mss", "match": match,
            "coverage": {"complete": True}, "total": len(rows), "sessions": rows}


def row(harness="claude", session_id="gold", indices=None):
    indices = [2] if indices is None else indices
    return {"session": {"harness": harness, "id": session_id},
            "hit_count": len(indices), "matched_indices": indices}


class BenchmarkConsumerTests(unittest.TestCase):
    def setUp(self):
        self.case = benchmark.Query("gold", "orchid semaphore", "found", {"claude:gold": [2]})

    def test_wrong_session_is_a_false_positive_and_missing_gold_is_a_false_negative(self):
        result = benchmark.evaluate_query(self.case, envelope(rows=[row(session_id="distractor")]))
        self.assertEqual(result["status"], "FAIL")
        self.assertEqual(result["false_positives"], 1)
        self.assertEqual(result["false_negatives"], 1)
        self.assertEqual(result["precision"], 0)
        self.assertEqual(result["recall"], 0)

    def test_candidate_passages_are_not_affirmative_evidence(self):
        case = benchmark.Query("near", "unmatched question", "candidates", {})
        result = benchmark.evaluate_query(case, envelope("candidates", [row()]))
        self.assertEqual(result["status"], "PASS")
        self.assertEqual(result["candidate_sessions"], 1)
        self.assertEqual(result["true_positives"], 0)
        self.assertEqual(result["false_positives"], 0)
        self.assertIsNone(result["precision"])
        # Relabeling the same candidate list as found must fail the frozen gold.
        result = benchmark.evaluate_query(case, envelope("found", [row()]))
        self.assertEqual(result["status"], "FAIL")
        self.assertEqual(result["false_positives"], 1)

    def test_wrong_indices_or_harness_do_not_pass_identity_only(self):
        wrong_index = benchmark.evaluate_query(self.case, envelope(rows=[row(indices=[1])]))
        self.assertEqual(wrong_index["status"], "FAIL")
        self.assertEqual(wrong_index["wrong_indices"]["claude:gold"]["actual"], [1])
        wrong_harness = benchmark.evaluate_query(self.case, envelope(rows=[row(harness="codex")]))
        self.assertEqual(wrong_harness["status"], "FAIL")
        self.assertEqual(wrong_harness["false_positives"], 1)

    def test_cap_cannot_masquerade_as_complete_recall(self):
        value = envelope(rows=[row()])
        value.update({"capped": True, "total": 900})
        self.assertEqual(benchmark.evaluate_query(self.case, value)["status"], "FAIL")

    def test_consumes_nested_session_envelope_and_rejects_broken_contracts(self):
        valid = envelope(rows=[row()])
        self.assertEqual(benchmark.read_session_rows(valid), {"claude:gold": [2]})
        malformed = []
        flat = copy.deepcopy(valid)
        flat["sessions"][0] = {"harness": "claude", "id": "gold", "matched_indices": [2], "hit_count": 1}
        malformed.append(flat)
        missing_coverage = copy.deepcopy(valid)
        del missing_coverage["coverage"]
        malformed.append(missing_coverage)
        mismatch_count = copy.deepcopy(valid)
        mismatch_count["sessions"][0]["hit_count"] = 4
        malformed.append(mismatch_count)
        duplicate = copy.deepcopy(valid)
        duplicate["sessions"].append(copy.deepcopy(duplicate["sessions"][0]))
        duplicate["total"] = 2
        malformed.append(duplicate)
        for value in malformed:
            with self.subTest(value=value), self.assertRaises(benchmark.BenchmarkFailure):
                benchmark.read_session_rows(value)

    def test_source_snapshot_detects_replacement_addition_and_deletion(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "source.jsonl"
            source.write_bytes(b"first\n")
            original = benchmark.snapshot(root)
            source.write_bytes(b"other\n")  # Same length, different bytes.
            replaced = benchmark.snapshot(root)
            self.assertNotEqual(original["sha256"], replaced["sha256"])
            extra = root / "extra.jsonl"
            extra.write_bytes(b"new\n")
            added = benchmark.snapshot(root)
            self.assertNotEqual(replaced["sha256"], added["sha256"])
            extra.unlink()
            self.assertEqual(benchmark.snapshot(root), replaced)
            source.unlink()
            self.assertEqual(benchmark.snapshot(root)["files"], 0)

    def test_isolation_does_not_inherit_private_roots_policy_or_redaction_override(self):
        inherited = {"PATH": "/safe/bin", "HOME": "/private/home", "USERPROFILE": "/private/home",
                     "XDG_CONFIG_HOME": "/private/config", "MSS_STORES": "opencode",
                     "MSS_CLAUDE_ROOT": "/private/claude", "MSS_XCODE_CODEX_ROOT": "/private/codex",
                     "MSS_NO_REDACT": "1", "MSS_EXCLUDE_PROJECTS": "*", "CODEX_HOME": "/private/codex"}
        with tempfile.TemporaryDirectory() as directory:
            workspace = Path(directory).resolve()
            env = benchmark.isolated_environment(workspace, inherited)
            self.assertEqual(env["PATH"], "/safe/bin")
            self.assertEqual(env["MSS_STORES"], "claude,codex,pi,omp")
            self.assertNotIn("MSS_NO_REDACT", env)
            self.assertNotIn("MSS_EXCLUDE_PROJECTS", env)
            for key in (*benchmark.ROOT_OVERRIDES, "HOME", "USERPROFILE", "XDG_CONFIG_HOME", "MSS_INDEX_DIR"):
                self.assertTrue(Path(env[key]).is_relative_to(workspace), key)
            self.assertTrue(Path(env["MSS_INDEX_DIR"]).is_absolute())

    def test_generator_is_reproducible_and_scaled_distractors_do_not_expand_gold(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            first = benchmark.generate_corpus(root / "first", 64)
            second = benchmark.generate_corpus(root / "second", 64)
            scaled = benchmark.generate_corpus(root / "scaled", 128)
            self.assertEqual(benchmark.snapshot(root / "first"), benchmark.snapshot(root / "second"))
            self.assertEqual([session.path.read_bytes() for session in first],
                             [session.path.read_bytes() for session in scaled[:64]])
            queries = benchmark.labeled_queries(first)
            self.assertEqual([len(case.gold) for case in queries], [32, 16, 16, 8, 16, 16, 4, 1, 16, 0, 0, 0, 0])
            self.assertEqual([case.gold for case in queries], [case.gold for case in benchmark.labeled_queries(scaled)])
            self.assertEqual({session.harness for session in second}, set(benchmark.HARNESSES))

    def test_public_report_removes_only_local_paths_and_preserves_failure_evidence(self):
        workspace = Path("/private/temporary/synthetic")
        binary = Path("/private/user/project/mss")
        report = {
            "status": "FAIL",
            "environment": {"binary_build_info": f"{binary}: go1.27.1"},
            "invocations": [{"stderr": f"cannot read {workspace}/sources/pi/log.jsonl: invalid JSON"}],
            "timings": {"first_fresh_index_seconds": 1.25},
        }
        benchmark.sanitize_report(report, workspace, binary)
        self.assertEqual(report["status"], "FAIL")
        self.assertEqual(report["environment"]["binary_build_info"], "<mss-binary>: go1.27.1")
        self.assertEqual(report["invocations"][0]["stderr"],
                         "cannot read <synthetic-root>/sources/pi/log.jsonl: invalid JSON")
        self.assertEqual(report["timings"]["first_fresh_index_seconds"], 1.25)

    def test_percentiles_use_documented_nearest_rank(self):
        self.assertEqual(benchmark.percentile([4, 1, 3, 2], .5), 2)
        self.assertEqual(benchmark.percentile([4, 1, 3, 2], .95), 4)


if __name__ == "__main__":
    unittest.main()
