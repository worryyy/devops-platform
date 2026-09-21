# syntax=docker/dockerfile:1
# Weekly report runner (P3): python + aggregate.py, launched as a Job by
# platform-server's report cron. Built and relayed through ACR as
# pulseops/report-runner:dev.
FROM python:3.12-alpine

ENV PIP_INDEX_URL=https://pypi.tuna.tsinghua.edu.cn/simple \
    PYTHONUNBUFFERED=1

RUN pip install --no-cache-dir boto3 jinja2

WORKDIR /app

COPY monitoring/report/aggregate.py /app/aggregate.py
COPY monitoring/report/templates/ /app/templates/

RUN adduser -D -u 65532 runner
USER runner

ENTRYPOINT ["python3", "/app/aggregate.py"]
