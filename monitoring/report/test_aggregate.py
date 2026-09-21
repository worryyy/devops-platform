#!/usr/bin/env python3
"""Fixture test for aggregate.py: run the weekly aggregator end-to-end against
local mocks (Prometheus, platform API, Feishu app, S3/MinIO) and assert the
key report fields. Stdlib only — it runs both locally and in the docker
pinned-image invoked by k3s/ci/scripts/test-observability.sh."""

from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import threading
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

HERE = Path(__file__).parent
FIXTURES = HERE.parent.parent / "k3s" / "ci" / "fixtures" / "report"


class MockState:
    def __init__(self) -> None:
        self.feishu_messages: list[dict] = []
        self.completion: dict | None = None
        self.uploads: list[str] = []


STATE = MockState()

PLATFORM_STATS = json.loads((FIXTURES / "platform-stats.json").read_text())

# Instant-query results keyed by a recognizable query fragment.
PROM_RESULTS = {
    'increase(ecampus_http_requests_total{outcome="failure"}': [
        {"metric": {"service": "theme"}, "value": [1789, "12"]},
        {"metric": {"service": "chat"}, "value": [1789, "0"]},
    ],
    "increase(ecampus_http_requests_total": [
        {"metric": {"service": "theme"}, "value": [1789, "1200"]},
        {"metric": {"service": "chat"}, "value": [1789, "300"]},
    ],
    "max_over_time(ecampus:http_request_duration_seconds:p95:5m": [
        {"metric": {"service": "theme"}, "value": [1789, "0.84"]},
        {"metric": {"service": "chat"}, "value": [1789, "1.2"]},
    ],
}


def prom_response(query: str) -> dict:
    for fragment, result in PROM_RESULTS.items():
        if fragment in query:
            return {"status": "success", "data": {"resultType": "vector", "result": result}}
    return {"status": "success", "data": {"resultType": "vector", "result": []}}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):  # silence
        pass

    def _json(self, payload: dict, status: int = 200) -> None:
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        if parsed.path == "/api/v1/query":
            query = urllib.parse.parse_qs(parsed.query).get("query", [""])[0]
            self._json(prom_response(query))
            return
        if parsed.path == "/api/reports/stats":
            self._json({"code": 0, "message": "ok", "data": PLATFORM_STATS})
            return
        if parsed.path == "/open-apis/im/v1/chats":
            self._json({"code": 0, "msg": "ok",
                        "data": {"items": [{"chat_id": "oc-test", "name": "alerts"}]}})
            return
        self._json({"code": 0, "msg": "ok", "data": {}})

    def do_HEAD(self):
        # S3 head_bucket
        self.send_response(200)
        self.send_header("x-amz-bucket-region", "us-east-1")
        self.end_headers()

    def do_PUT(self):
        length = int(self.headers.get("Content-Length", 0) or 0)
        if length:
            self.rfile.read(length)
        STATE.uploads.append(self.path)
        self.send_response(200)
        self.send_header("ETag", '"d41d8cd98f00b204e9800998ecf8427e"')
        self.send_header("Content-Length", "0")
        self.end_headers()

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0) or 0)
        body = self.rfile.read(length) if length else b"{}"
        if self.path.startswith("/open-apis/auth/v3/tenant_access_token"):
            self._json({"code": 0, "msg": "ok",
                        "tenant_access_token": "t-test", "expire": 7200})
            return
        if self.path.startswith("/open-apis/im/v1/messages"):
            STATE.feishu_messages.append(json.loads(body))
            self._json({"code": 0, "msg": "ok", "data": {"message_id": "m1"}})
            return
        if self.path == "/api/webhooks/report-runner":
            STATE.completion = json.loads(body)
            self._json({"code": 0, "message": "ok", "data": {"ok": True}})
            return
        self._json({"code": 0, "msg": "ok"})


def main() -> int:
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    port = server.server_address[1]
    threading.Thread(target=server.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{port}"

    with tempfile.TemporaryDirectory() as out:
        env = dict(os.environ)
        env.update({
            "PERIOD": "2026-W37",
            "WEEK_START": "2026-09-07T00:00:00Z",
            "WEEK_END": "2026-09-14T00:00:00Z",
            "REPORT_RUN_ID": "77",
            "PROMETHEUS_URL": base,
            "PLATFORM_API_URL": base + "/api",  # stats + webhook share the mock
            "PLATFORM_WEBHOOK_SECRET": "sec",
            "FEISHU_APP_ID": "cli_test",
            "FEISHU_APP_SECRET": "appsec",
            "FEISHU_BASE_URL": base,
            "PLATFORM_PUBLIC_URL": "http://platform.test",
            "MINIO_ENDPOINT": f"127.0.0.1:{port}",
            "MINIO_ACCESS_KEY": "ak",
            "MINIO_SECRET_KEY": "sk",
            "MINIO_BUCKET": "reports",
            "MINIO_SECURE": "false",
            "OUT_DIR": out,
        })
        proc = subprocess.run(
            [sys.executable, str(HERE / "aggregate.py")],
            env=env, capture_output=True, text=True, timeout=120,
        )
        if proc.returncode != 0:
            print(proc.stdout)
            print(proc.stderr, file=sys.stderr)
            print("aggregate.py exited non-zero", file=sys.stderr)
            return 1

        report = json.loads((Path(out) / "report.json").read_text())
        html = (Path(out) / "report.html").read_text()

        failures = []
        def expect(cond: bool, message: str) -> None:
            if not cond:
                failures.append(message)

        expect(report["period"] == "2026-W37", "period")
        expect(not report["gaps"], f"gaps present: {report['gaps']}")
        sla = {row["service"]: row for row in report["sla"]}
        expect(set(sla) == {"theme", "chat"}, f"sla services {sorted(sla)}")
        expect(abs(sla["theme"]["success_ratio"] - (1 - 12 / 1200)) < 1e-9,
               f"theme success_ratio {sla['theme'].get('success_ratio')}")
        expect(sla["chat"]["p95_max"] == 1.2, "chat p95")
        expect(report["platform"]["releases"]["total"] == 6, "releases total")
        expect(report["platform"]["alerts"]["top"][0]["alertname"] == "ReleasePodRestarting",
               "alert top1")
        for needle in ("2026-W37", "theme", "发布成功率", "ReleasePodRestarting", "服务可用性"):
            expect(needle in html, f"html missing {needle!r}")

        expect(len(STATE.uploads) == 2, f"uploads {STATE.uploads}")
        expect(any(u.endswith("/report.html") for u in STATE.uploads), "html uploaded")

        expect(STATE.completion is not None, "no completion callback")
        if STATE.completion:
            expect(STATE.completion["runId"] == 77, "callback runId")
            expect(STATE.completion["status"] == "success",
                   f"callback status {STATE.completion['status']}")
            expect(STATE.completion["objectPath"] == "weekly/2026-W37/report.html", "callback path")

        expect(len(STATE.feishu_messages) == 1, f"feishu messages {len(STATE.feishu_messages)}")
        if STATE.feishu_messages:
            content = json.loads(STATE.feishu_messages[0]["content"])
            text = content.get("text", "")
            for needle in ("2026-W37", "发布：6 次", "83.3%", "http://platform.test/reports"):
                expect(needle in text, f"feishu summary missing {needle!r}")

        if failures:
            for failure in failures:
                print(f"FAIL: {failure}", file=sys.stderr)
            print(proc.stderr, file=sys.stderr)
            return 1
    print("aggregate fixtures OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
