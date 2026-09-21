import { useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Alert as AntAlert, Card, Empty, Input, Select, Space, Table, Tabs, Tag, Typography } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { logsApi, type HistogramPoint, type LogRow, type PatternRow } from '../api/logs';
import { servicesApi } from '../api/services';

const LEVELS = ['DEBUG', 'INFO', 'WARN', 'ERROR', 'FATAL'];

const LEVEL_COLOR: Record<string, string> = {
  ERROR: 'red',
  FATAL: 'red',
  WARN: 'orange',
  INFO: 'blue',
  DEBUG: 'default',
};

const RANGES = [
  { key: '15m', label: '近 15 分钟', minutes: 15 },
  { key: '1h', label: '近 1 小时', minutes: 60 },
  { key: '6h', label: '近 6 小时', minutes: 360 },
  { key: '24h', label: '近 24 小时', minutes: 1440 },
];

function fmtTime(v: string) {
  if (!v) return '-';
  return new Date(v).toLocaleString('zh-CN', { hour12: false });
}

// 轻量分钟直方图（无第三方图表依赖；高度按最大计数归一）
function Histogram({ points }: { points: HistogramPoint[] }) {
  if (!points.length) return <Empty description="窗口内无日志" image={Empty.PRESENTED_IMAGE_SIMPLE} />;
  const max = Math.max(...points.map((p) => p.count), 1);
  const start = new Date(points[0].minute).getTime();
  const end = new Date(points[points.length - 1].minute).getTime();
  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'flex-end', gap: 1, height: 80 }}>
        {points.map((p) => (
          <div
            key={p.minute}
            title={`${fmtTime(p.minute)} · ${p.count} 条`}
            style={{
              flex: 1,
              minWidth: 2,
              height: `${Math.max((p.count / max) * 100, 3)}%`,
              background: p.count / max > 0.66 ? '#cf1322' : '#1677ff',
              opacity: 0.8,
              borderRadius: 1,
            }}
          />
        ))}
      </div>
      <div style={{ display: 'flex', justifyContent: 'space-between', color: '#888', fontSize: 12, marginTop: 4 }}>
        <span>{fmtTime(new Date(start).toISOString())}</span>
        <span>峰值 {max}/min</span>
        <span>{fmtTime(new Date(end).toISOString())}</span>
      </div>
    </div>
  );
}

export default function Logs() {
  // 深链预填：/logs?trace_id=xx&service=yy&level=ERROR（P5 卡片跳转入口）
  const [searchParams] = useSearchParams();
  const [service, setService] = useState(searchParams.get('service') ?? '');
  const [level, setLevel] = useState(searchParams.get('level') ?? '');
  const [traceId, setTraceId] = useState(searchParams.get('trace_id') ?? '');
  const [keyword, setKeyword] = useState(searchParams.get('q') ?? '');
  const [rangeKey, setRangeKey] = useState('1h');
  const [tab, setTab] = useState('raw');

  const range = RANGES.find((r) => r.key === rangeKey) ?? RANGES[1];
  const window = useMemo(() => {
    const end = new Date();
    const start = new Date(end.getTime() - range.minutes * 60_000);
    return { start: start.toISOString(), end: end.toISOString() };
  }, [range]);

  const params = useMemo(
    () => ({
      service: service || undefined,
      level: level || undefined,
      trace_id: traceId || undefined,
      q: keyword || undefined,
      start: window.start,
      end: window.end,
    }),
    [service, level, traceId, keyword, window],
  );

  const services = useQuery({ queryKey: ['services'], queryFn: () => servicesApi.list() });

  const histogram = useQuery({
    queryKey: ['logs-histogram', params],
    queryFn: () => logsApi.histogram(params),
    refetchInterval: 15000,
  });

  const rows = useQuery({
    queryKey: ['logs', params],
    queryFn: () => logsApi.search({ ...params, limit: 200 }),
    refetchInterval: 15000,
    enabled: tab === 'raw',
  });

  const patterns = useQuery({
    queryKey: ['logs-patterns', params],
    queryFn: () => logsApi.patterns(params),
    refetchInterval: 30000,
    enabled: tab === 'patterns',
  });

  const rawColumns = [
    { title: '时间', dataIndex: 'ts', key: 'ts', width: 180, render: fmtTime },
    {
      title: '级别',
      dataIndex: 'level',
      key: 'level',
      width: 80,
      render: (v: string) => <Tag color={LEVEL_COLOR[v] ?? 'default'}>{v}</Tag>,
    },
    { title: '服务', dataIndex: 'service', key: 'service', width: 130 },
    { title: '路由', dataIndex: 'route', key: 'route', width: 160, render: (v: string) => v || '-' },
    {
      title: '信息',
      dataIndex: 'msg',
      key: 'msg',
      render: (v: string, row: LogRow) => (
        <Typography.Text code copyable={row.trace_id ? { text: row.trace_id } : false} style={{ fontSize: 12 }}>
          {row.trace_id ? `[${row.trace_id.slice(0, 8)}] ` : ''}
          {v}
        </Typography.Text>
      ),
    },
    { title: 'Pod', dataIndex: 'pod', key: 'pod', width: 200, ellipsis: true },
  ];

  const patternColumns = [
    { title: '#', key: 'idx', width: 44, render: (_: unknown, __: unknown, i: number) => i + 1 },
    { title: '次数', dataIndex: 'count', key: 'count', width: 90 },
    {
      title: '首见',
      dataIndex: 'first_seen',
      key: 'first_seen',
      width: 180,
      render: fmtTime,
    },
    { title: '末见', dataIndex: 'last_seen', key: 'last_seen', width: 180, render: fmtTime },
    { title: '模式', dataIndex: 'pattern', key: 'pattern', render: (v: string) => <Typography.Text code>{v}</Typography.Text> },
    { title: 'Pods', dataIndex: 'pods', key: 'pods', width: 220, render: (v: string[]) => (v ?? []).join(', ') },
  ];

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Card size="small">
        <Space wrap size="middle">
          <Select
            style={{ width: 170 }}
            placeholder="服务"
            allowClear
            showSearch
            value={service || undefined}
            onChange={(v) => setService(v ?? '')}
            options={(services.data ?? []).map((s) => ({ value: s.name, label: s.name }))}
          />
          <Select
            style={{ width: 100 }}
            placeholder="级别"
            allowClear
            value={level || undefined}
            onChange={(v) => setLevel(v ?? '')}
            options={LEVELS.map((l) => ({ value: l, label: l }))}
          />
          <Select
            style={{ width: 130 }}
            value={rangeKey}
            onChange={setRangeKey}
            options={RANGES.map((r) => ({ value: r.key, label: r.label }))}
          />
          <Input
            style={{ width: 220 }}
            placeholder="关键字（msg 子串）"
            allowClear
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
          />
          <Input
            style={{ width: 280 }}
            placeholder="trace_id 精确查询"
            allowClear
            value={traceId}
            onChange={(e) => setTraceId(e.target.value.trim())}
          />
        </Space>
      </Card>

      <Card size="small" title="时间直方图（分钟）">
        {histogram.isError ? (
          <AntAlert type="warning" showIcon message="ClickHouse 查询失败（管道未就绪或不可达）" />
        ) : (
          <Histogram points={histogram.data ?? []} />
        )}
      </Card>

      <Card size="small">
        <Tabs
          activeKey={tab}
          onChange={setTab}
          items={[
            {
              key: 'raw',
              label: `原始日志${rows.data ? `（${rows.data.length}）` : ''}`,
              children: (
                <Table<LogRow>
                  size="small"
                  rowKey={(r) => `${r.ts}-${r.service}-${r.msg}`}
                  loading={rows.isLoading}
                  columns={rawColumns}
                  dataSource={rows.data ?? []}
                  pagination={{ pageSize: 50, showSizeChanger: false }}
                  locale={{ emptyText: <Empty description="无匹配日志" image={Empty.PRESENTED_IMAGE_SIMPLE} /> }}
                />
              ),
            },
            {
              key: 'patterns',
              label: '模式聚合（Top 10）',
              children: (
                <>
                  <Table<PatternRow>
                    size="small"
                    rowKey="pattern"
                    loading={patterns.isLoading}
                    columns={patternColumns}
                    dataSource={patterns.data ?? []}
                    pagination={false}
                    expandable={{
                      expandedRowRender: (r) => (
                        <Typography.Paragraph code copyable style={{ marginBottom: 0, whiteSpace: 'pre-wrap' }}>
                          {r.sample}
                        </Typography.Paragraph>
                      ),
                    }}
                  />
                  {patterns.data && patterns.data.length === 0 && (
                    <Empty description="窗口内无日志可聚合" image={Empty.PRESENTED_IMAGE_SIMPLE} />
                  )}
                </>
              ),
            },
          ]}
        />
      </Card>
    </Space>
  );
}
