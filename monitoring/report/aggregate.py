#!/usr/bin/env python3
"""Weekly observability report aggregator (P3 v1).

Collects one week of platform data, renders report.json + report.html,
uploads them to MinIO and pushes a Feishu summary. Data sources are
independent: a failing source degrades into a noted gap instead of failing
the whole report (blueprint §7.1).

Environment:
  PERIOD             e.g. 2026-W38
  WEEK_START/WEEK_END  RFC3339 window bounds
  REPORT_RUN_ID      report_runs row id for the completion callback
  PLATFORM_API_URL   e.g. http://platform-server-api.platform.svc/api
  PLATFORM_WEBHOOK_SECRET
  PROMETHEUS_URL     e.g. http://prometheus-monitoring.svc:9090
  MINIO_ENDPOINT     host:port (scheme-less, path-style)
  MINIO_ACCESS_KEY / MINIO_SECRET_KEY / MINIO_BUCKET
  MINIO_SECURE       "false" in-cluster
  FEISHU_WEBHOOK_URL / FEISHU_WEBHOOK_SECRET (optional)
  PLATFORM_PUBLIC_URL  e.g. http://platform.100.115.204.94.nip.io
"""

from __future__ import annotations

import base64
import hashlib
import hmac
import json
import os
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

import boto3  # type: ignore
import jinja2
from botocore.client import Config as BotoConfig  # type: ignore

OUT_DIR = Path(os.environ.get("OUT_DIR", "/tmp/out"))  # writable for nonroot runners
TEMPLATE = Path(__file__).parent / "templates" / "report.html.j2"


def env(name: str, default: str = "") -> str:
    return os.environ.get(name, default).strip()


def http_json(url: str, method: str = "GET", body: dict | None = None,
              headers: dict | None = None, timeout: int = 20) -> dict:
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    for key, value in (headers or {}).items():
        req.add_header(key, value)
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read().decode())


def iso(dt: str) -> datetime:
    return datetime.fromisoformat(dt.replace("Z", "+00:00"))


def rfc3339(dt: datetime) -> str:
    return dt.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


# ---------------------------------------------------------------- Prometheus

def prom_instant(base: str, query: str, at: datetime) -> dict:
    url = f"{base}/api/v1/query?query={urllib.request.quote(query)}&time={at.timestamp():.0f}"
    payload = http_json(url)
    if payload.get("status") != "success":
        raise RuntimeError(f"prometheus status {payload.get('status')}")
    return payload.get("data", {}).get("result", [])


def collect_prometheus(report: dict, base: str, start: datetime, end: datetime) -> None:
    """Per-service weekly availability, error totals and P95 peaks."""
    window = f"[{int((end - start).total_seconds())}s]"
    services: dict[str, dict] = {}

    def bucket(name: str) -> dict:
        return services.setdefault(name, {"service": name})

    # Success/error volume per service over the window (increase at `end`).
    for metric, field in (("ecampus_http_requests_total", "requests"),
                          ("ecampus_http_requests_total{outcome=\"failure\"}", "errors")):
        result = prom_instant(
            base,
            f"sum by (service) (increase({metric}{window}))",
            end,
        )
        for series in result:
            metric_set = series["metric"]
            name = metric_set.get("service")
            if not name:
                continue
            bucket(name)[field] = float(series["value"][1])

    # Weekly P95 peak per service from the recording rule.
    result = prom_instant(
        base,
        f"max by (service) (max_over_time(ecampus:http_request_duration_seconds:p95:5m{window}))",
        end,
    )
    for series in result:
        name = series["metric"].get("service")
        if name:
            bucket(name)["p95_max"] = float(series["value"][1])

    rows = []
    for name in sorted(services):
        entry = services[name]
        requests = entry.get("requests", 0.0)
        errors = entry.get("errors", 0.0)
        entry["success_ratio"] = (1 - errors / requests) if requests > 0 else None
        rows.append(entry)
    report["sla"] = rows


# -------------------------------------------------------------------- platform

def collect_platform(report: dict, api: str, secret: str, start: datetime, end: datetime) -> None:
    stats = http_json(
        f"{api}/reports/stats?start={rfc3339(start)}&end={rfc3339(end)}",
        headers={"X-Platform-Webhook": secret},
    )
    report["platform"] = stats.get("data", stats)


# --------------------------------------------------------------------- MinIO

def upload(report_dir: Path, endpoint: str, access_key: str, secret_key: str,
           bucket: str, secure: bool, prefix: str) -> str:
    scheme = "https" if secure else "http"
    client = boto3.client(
        "s3",
        endpoint_url=f"{scheme}://{endpoint}",
        aws_access_key_id=access_key,
        aws_secret_access_key=secret_key,
        config=BotoConfig(signature_version="s3v4", s3={"addressing_style": "path"}),
        region_name="us-east-1",
    )
    try:
        client.head_bucket(Bucket=bucket)
    except Exception:
        client.create_bucket(Bucket=bucket)
    for name in ("report.html", "report.json"):
        client.upload_file(
            str(report_dir / name), bucket, f"{prefix}/{name}",
            ExtraArgs={"ContentType": "text/html; charset=utf-8"} if name.endswith(".html") else None,
        )
    return f"{prefix}/report.html"


# --------------------------------------------------------------------- Feishu

FEISHU_BASE = env("FEISHU_BASE_URL", "https://open.feishu.cn")
FEISHU_STATE: dict = {"token": "", "expire": 0.0}


def feishu_sign(secret: str, timestamp: int) -> str:
    string_to_sign = f"{timestamp}\n{secret}"
    digest = hmac.new(string_to_sign.encode(), b"", hashlib.sha256).digest()
    return base64.b64encode(digest).decode()


def feishu_token(app_id: str, app_secret: str) -> str:
    now = time.time()
    if FEISHU_STATE["token"] and now < FEISHU_STATE["expire"]:
        return FEISHU_STATE["token"]
    payload = http_json(
        f"{FEISHU_BASE}/open-apis/auth/v3/tenant_access_token/internal",
        method="POST", body={"app_id": app_id, "app_secret": app_secret},
    )
    FEISHU_STATE["token"] = payload["tenant_access_token"]
    FEISHU_STATE["expire"] = now + payload.get("expire", 7200) - 60
    return FEISHU_STATE["token"]


def push_feishu(text: str) -> None:
    """App mode (app id/secret) wins; bot webhook is the fallback."""
    app_id, app_secret = env("FEISHU_APP_ID"), env("FEISHU_APP_SECRET")
    try:
        if app_id and app_secret:
            token = feishu_token(app_id, app_secret)
            chat_id = env("FEISHU_CHAT_ID")
            if not chat_id:
                chats = http_json(f"{FEISHU_BASE}/open-apis/im/v1/chats",
                                  headers={"Authorization": f"Bearer {token}"})
                items = chats.get("data", {}).get("items", [])
                if len(items) != 1:
                    print(f"feishu chat discovery found {len(items)} chats; set FEISHU_CHAT_ID",
                          file=sys.stderr)
                    return
                chat_id = items[0]["chat_id"]
            req = urllib.request.Request(
                f"{FEISHU_BASE}/open-apis/im/v1/messages?receive_id_type=chat_id",
                data=json.dumps({"receive_id": chat_id, "msg_type": "text",
                                 "content": json.dumps({"text": text})}).encode(),
                method="POST")
            req.add_header("Authorization", f"Bearer {token}")
        else:
            webhook = env("FEISHU_WEBHOOK_URL")
            if not webhook:
                return
            payload: dict = {"msg_type": "text", "content": {"text": text}}
            secret = env("FEISHU_WEBHOOK_SECRET")
            if secret:
                now = int(time.time())
                payload["timestamp"] = str(now)
                payload["sign"] = feishu_sign(secret, now)
            req = urllib.request.Request(webhook, data=json.dumps(payload).encode(), method="POST")
        req.add_header("Content-Type", "application/json; charset=utf-8")
        with urllib.request.urlopen(req, timeout=15) as resp:
            resp.read()
    except urllib.error.HTTPError as exc:  # noqa: BLE001 - report must not die here
        print(f"feishu push failed: {exc}", file=sys.stderr)
    except Exception as exc:  # noqa: BLE001
        print(f"feishu push failed: {exc}", file=sys.stderr)


def build_summary(period: str, report: dict, public_url: str, object_path: str) -> str:
    platform = report.get("platform", {})
    releases = platform.get("releases", {})
    pipelines = platform.get("pipelines", {})
    alerts = platform.get("alerts", {})
    total = releases.get("total", 0)
    stable = releases.get("stable", 0)
    rate = f"{stable / total * 100:.1f}%" if total else "n/a"
    top = alerts.get("top") or []
    top_line = top[0] if top else None
    lines = [
        f"📊 周报 {period}",
        f"发布：{total} 次，成功率 {rate}（stable {stable}/failed {releases.get('failed', 0)}）",
        f"流水线：{pipelines.get('total', 0)} 次（成功 {pipelines.get('success', 0)} / 失败 {pipelines.get('failed', 0)}）",
        f"告警：{alerts.get('total', 0)} 条"
        + (f"，最多：{top_line['alertname']}({top_line['service']}) ×{top_line['count']}" if top_line else ""),
    ]
    if public_url:
        lines.append(f"查看：{public_url}/reports")
    if object_path:
        lines.append(f"归档：{object_path}")
    return "\n".join(lines)


# ----------------------------------------------------------------------- main

def main() -> int:
    period = env("PERIOD")
    start = iso(env("WEEK_START"))
    end = iso(env("WEEK_END"))
    run_id = env("REPORT_RUN_ID")
    api = env("PLATFORM_API_URL")
    secret = env("PLATFORM_WEBHOOK_SECRET")
    public_url = env("PLATFORM_PUBLIC_URL")

    report: dict = {"period": period, "generated_at": rfc3339(datetime.now(timezone.utc)),
                    "window": {"start": rfc3339(start), "end": rfc3339(end)}, "gaps": []}

    try:
        collect_prometheus(report, env("PROMETHEUS_URL"), start, end)
    except Exception as exc:  # noqa: BLE001 - degrade with a noted gap
        report["gaps"].append(f"prometheus: {exc}")
        print(f"prometheus collection failed: {exc}", file=sys.stderr)
        report["sla"] = []

    try:
        collect_platform(report, api, secret, start, end)
    except Exception as exc:  # noqa: BLE001
        report["gaps"].append(f"platform-stats: {exc}")
        print(f"platform stats failed: {exc}", file=sys.stderr)
        report["platform"] = {"releases": {}, "pipelines": {}, "alerts": {}}

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    (OUT_DIR / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2))
    html = jinja2.Template(TEMPLATE.read_text()).render(report=report)
    (OUT_DIR / "report.html").write_text(html)

    object_path = ""
    try:
        object_path = upload(
            OUT_DIR, env("MINIO_ENDPOINT"), env("MINIO_ACCESS_KEY"), env("MINIO_SECRET_KEY"),
            env("MINIO_BUCKET", "reports"), env("MINIO_SECURE", "false") == "true",
            f"weekly/{period}",
        )
    except Exception as exc:  # noqa: BLE001
        report["gaps"].append(f"minio: {exc}")
        print(f"minio upload failed: {exc}", file=sys.stderr)

    webhook = env("FEISHU_WEBHOOK_URL")
    if env("FEISHU_APP_ID") or webhook:
        push_feishu(build_summary(period, report, public_url, object_path))

    status = "success" if not report["gaps"] else ("success" if object_path else "failed")
    if api and run_id:
        try:
            http_json(
                f"{api}/webhooks/report-runner", method="POST",
                body={"runId": int(run_id), "status": status,
                      "objectPath": object_path,
                      "error": "; ".join(report["gaps"]) or ""},
                headers={"X-Platform-Webhook": secret},
            )
        except Exception as exc:  # noqa: BLE001
            print(f"completion callback failed: {exc}", file=sys.stderr)
    print(f"report {period}: {status} ({object_path or 'no object'})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
