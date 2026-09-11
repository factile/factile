#!/usr/bin/env python3
"""Compare logical read/edit payloads; no timing or agent-preference claims."""
import json
import pathlib
import shutil
import subprocess
import sys
import tempfile

binary = str(pathlib.Path(sys.argv[1]).resolve())
fixture = pathlib.Path(__file__).resolve().parents[1] / "testdata/bundles/editing"

# Each direct edit lists the literal replacements a file-edit tool would receive.
scenarios = {
    "sentence": [("guide", [{"op": "replace_text", "old": "An old sentence.", "new": "A new sentence."}], [("An old sentence.", "A new sentence.")])],
    "link": [("guide", [{"op": "replace_text", "old": "](/old)", "new": "](/new)"}], [("](/old)", "](/new)")])],
    "metadata": [("guide", [{"op": "set", "key": "status", "value": "active"}], [("status: draft", "status: active")])],
    "multi_section": [("guide", [{"op": "replace_text", "old": "Do this.", "new": "Do that."}, {"op": "replace_text", "old": "Keep this.", "new": "Keep that."}], [("Do this.", "Do that."), ("Keep this.", "Keep that.")])],
    "concept_index_log": [
        ("guide", [{"op": "replace_text", "old": "An old sentence.", "new": "A new sentence."}], [("An old sentence.", "A new sentence.")]),
        ("index", [{"op": "replace_text", "old": "[Guide]", "new": "[Updated guide]"}], [("[Guide]", "[Updated guide]")]),
        ("log", [{"op": "replace_text", "old": "Initial entry.", "new": "Initial entry.\nUpdated guide."}], [("Initial entry.", "Initial entry.\nUpdated guide.")]),
    ],
}


def encoded(value):
    return json.dumps(value, separators=(",", ":")).encode()


def run(workspace, *args, stdin=None):
    return subprocess.run([binary, "--workspace", str(workspace), *args, "--json"], input=stdin, check=True, capture_output=True).stdout


results = []
with tempfile.TemporaryDirectory(prefix="factile-edit-compare-") as temporary:
    for name, edits in scenarios.items():
        for mode in ("direct", "cli_brief", "cli_default"):
            workspace = pathlib.Path(temporary) / f"{name}-{mode}"
            shutil.copytree(fixture, workspace)
            # A moderate document makes full-document response cost visible.
            with (workspace / "guide.md").open("a") as stream:
                stream.write("\n## Reference\n" + "Unchanged reference material.\n" * 400)
            input_bytes = output_bytes = calls = 0
            for document, operations, replacements in edits:
                file = workspace / f"{document}.md"
                before = file.read_bytes()
                expected = before
                for old, new in replacements:
                    assert expected.count(old.encode()) == 1
                    expected = expected.replace(old.encode(), new.encode(), 1)
                if mode == "direct":
                    # Logical file read plus an exact-replacement edit request/ack.
                    input_bytes += len(encoded({"path": f"/{document}.md"}))
                    input_bytes += len(encoded({"path": f"/{document}.md", "replacements": replacements}))
                    output_bytes += len(before) + len(b"ok\n")
                    file.write_bytes(expected)
                else:
                    read = run(workspace, "read", f"/{document}")
                    request = {"expected_revision": json.loads(read)["concept"]["revision"], "operations": operations}
                    if mode == "cli_brief":
                        request["brief"] = True
                    payload = encoded(request)
                    response = run(workspace, "patch", f"/{document}", "--input", "-", stdin=payload)
                    input_bytes += len(encoded(["read", f"/{document}", "--json"]))
                    input_bytes += len(encoded(["patch", f"/{document}", "--input", "-", "--json"])) + len(payload)
                    output_bytes += len(read) + len(response)
                calls += 2
                assert file.read_bytes() == expected, (name, mode, "unexpected or unrelated diff")
            results.append({"scenario": name, "mode": mode, "calls": calls, "input_bytes": input_bytes, "output_bytes": output_bytes, "retries": 0, "correct": True, "unrelated_diff": False})
print(json.dumps(results, indent=2))
