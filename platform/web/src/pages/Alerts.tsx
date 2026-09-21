import { useState } from 'react';
import { Alert as AntAlert, Button, Descriptions, message, Select, Space, Table, Tag, Typography } from 'antd';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { alertsApi, type Alert } from '../api/alerts';

const STATUS_COLOR: Record<string, string> = {
  firing: 'error',
  resolved: 'success',
};

const STATUS_TEXT: Record<string, string> = {
  firing: '触发中',
  resolved: '已恢复',
};

const SEVERITY_COLOR: Record<string, string> = {
  critical: 'red',
  warning: 'orange',
  info: 'blue',
};

const SIGNAL_TYPE_TEXT: Record<string, string> = {
  user_impact: '用户影响',
  deploy_noise: '发布噪音',
  deploy_context: '发布上下文',
  infra: '基础设施',
};

function fmtTime(v: string) {
  if (!v) return '-';
  return new Date(v).toLocaleString('zh-CN', { hour12: false });
}

export default function Alerts() {
  const queryClient = useQueryClient();
  const [filters, setFilters] = useState<{ status?: string; severity?: string; signalType?: string }>({});

  const alerts = useQuery({
    queryKey: ['alerts', filters],
    queryFn: () => alertsApi.list({ ...filters, limit: 200 }),
    refetchInterval: 15000,
  });

  const ack = useMutation({
    mutationFn: (id: number) => alertsApi.ack(id),
    onSuccess: (_data, id) => {
      message.success(`已确认告警 #${id}`);
      queryClient.invalidateQueries({ queryKey: ['alerts'] });
    },
    onError: (error: any) => message.error(error?.response?.data?.message || '确认失败'),
  });

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 70 },
    { title: '告警', dataIndex: 'alertname', key: 'alertname' },
    {
      title: '服务',
      dataIndex: 'service',
      key: 'service',
      render: (v: string) => v || '-',
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 90,
      render: (s: string) => <Tag color={STATUS_COLOR[s] ?? 'default'}>{STATUS_TEXT[s] ?? s}</Tag>,
    },
    {
      title: '级别',
      dataIndex: 'severity',
      key: 'severity',
      width: 90,
      render: (s: string) => <Tag color={SEVERITY_COLOR[s] ?? 'default'}>{s || '-'}</Tag>,
    },
    {
      title: '信号类型',
      dataIndex: 'signalType',
      key: 'signalType',
      width: 110,
      render: (s: string) => (s ? <Tag>{SIGNAL_TYPE_TEXT[s] ?? s}</Tag> : '-'),
    },
    {
      title: '开始时间',
      dataIndex: 'startsAt',
      key: 'startsAt',
      width: 170,
      render: fmtTime,
    },
    {
      title: '接收时间',
      dataIndex: 'receivedAt',
      key: 'receivedAt',
      width: 170,
      render: fmtTime,
    },
    {
      title: '',
      key: 'actions',
      width: 90,
      render: (_: unknown, alert: Alert) =>
        alert.labels.set?.acked_by ? (
          <Tag color="green">已确认 {alert.labels.set.acked_by}</Tag>
        ) : (
          <Button size="small" onClick={() => ack.mutate(alert.id)} loading={ack.isPending}>
            确认
          </Button>
        ),
    },
  ];

  return (
    <div>
      <Typography.Title level={4} style={{ marginTop: 0 }}>
        告警
      </Typography.Title>

      <Space style={{ marginBottom: 16 }} wrap>
        <Select
          allowClear
          placeholder="状态"
          style={{ width: 120 }}
          value={filters.status}
          onChange={(v) => setFilters((f) => ({ ...f, status: v }))}
          options={[
            { value: 'firing', label: '触发中' },
            { value: 'resolved', label: '已恢复' },
          ]}
        />
        <Select
          allowClear
          placeholder="级别"
          style={{ width: 120 }}
          value={filters.severity}
          onChange={(v) => setFilters((f) => ({ ...f, severity: v }))}
          options={[
            { value: 'critical', label: 'critical' },
            { value: 'warning', label: 'warning' },
            { value: 'info', label: 'info' },
          ]}
        />
        <Select
          allowClear
          placeholder="信号类型"
          style={{ width: 140 }}
          value={filters.signalType}
          onChange={(v) => setFilters((f) => ({ ...f, signalType: v }))}
          options={Object.entries(SIGNAL_TYPE_TEXT).map(([value, label]) => ({ value, label }))}
        />
      </Space>

      {alerts.isError && (
        <AntAlert
          type="error"
          style={{ marginBottom: 16 }}
          message="告警列表加载失败"
          description={(alerts.error as Error)?.message}
        />
      )}

      <Table<Alert>
        rowKey="id"
        columns={columns}
        dataSource={alerts.data ?? []}
        loading={alerts.isLoading}
        pagination={{ pageSize: 20, showSizeChanger: false }}
        locale={{ emptyText: '暂无告警记录' }}
        expandable={{
          rowExpandable: () => true,
          expandedRowRender: (alert) => (
            <Descriptions size="small" column={2} bordered>
              <Descriptions.Item label="fingerprint" span={2}>
                <code>{alert.fingerprint}</code>
              </Descriptions.Item>
              <Descriptions.Item label="Labels" span={2}>
                <code style={{ whiteSpace: 'pre-wrap' }}>
                  {JSON.stringify(alert.labels.set ?? {}, null, 2)}
                </code>
              </Descriptions.Item>
              <Descriptions.Item label="Annotations" span={2}>
                <code style={{ whiteSpace: 'pre-wrap' }}>
                  {JSON.stringify(alert.annotations.set ?? {}, null, 2)}
                </code>
              </Descriptions.Item>
              <Descriptions.Item label="结束时间">{fmtTime(alert.endsAt)}</Descriptions.Item>
              <Descriptions.Item label="摘要">
                {alert.annotations.set?.summary || alert.annotations.set?.description || '-'}
              </Descriptions.Item>
            </Descriptions>
          ),
        }}
      />
    </div>
  );
}
