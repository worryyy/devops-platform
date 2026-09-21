import { useState } from 'react';
import {
  Alert as AntAlert,
  Button,
  Descriptions,
  message,
  Modal,
  Table,
  Tag,
  Typography,
} from 'antd';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { reportsApi, type ReportRun } from '../api/reports';

const STATUS_COLOR: Record<string, string> = {
  running: 'processing',
  success: 'success',
  failed: 'error',
};

const STATUS_TEXT: Record<string, string> = {
  running: '生成中',
  success: '成功',
  failed: '失败',
};

export default function Reports() {
  const queryClient = useQueryClient();
  const [preview, setPreview] = useState<ReportRun | null>(null);

  const runs = useQuery({
    queryKey: ['reports'],
    queryFn: () => reportsApi.list(),
    refetchInterval: 20000,
  });

  const trigger = useMutation({
    mutationFn: () => reportsApi.trigger(),
    onSuccess: (run) => {
      message.success(`已触发周报 ${run.period}（run #${run.id}）`);
      queryClient.invalidateQueries({ queryKey: ['reports'] });
    },
    onError: (error: any) => message.error(error?.response?.data?.message || '触发失败'),
  });

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 70 },
    { title: '周期', dataIndex: 'period', key: 'period' },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (s: string) => <Tag color={STATUS_COLOR[s] ?? 'default'}>{STATUS_TEXT[s] ?? s}</Tag>,
    },
    {
      title: '对象',
      dataIndex: 'objectPath',
      key: 'objectPath',
      render: (v: string) => (v ? <code>{v}</code> : '-'),
    },
    {
      title: '生成时间',
      dataIndex: 'createdAt',
      key: 'createdAt',
      width: 180,
      render: (v: string) => new Date(v).toLocaleString('zh-CN', { hour12: false }),
    },
    {
      title: '',
      key: 'actions',
      width: 120,
      render: (_: unknown, run: ReportRun) => (
        <Button
          size="small"
          disabled={!run.objectPath}
          onClick={() => setPreview(run)}
        >
          在线预览
        </Button>
      ),
    },
  ];

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Typography.Title level={4} style={{ marginTop: 0 }}>
          周报
        </Typography.Title>
        <Button type="primary" loading={trigger.isPending} onClick={() => trigger.mutate()}>
          手动生成（上一周）
        </Button>
      </div>

      {runs.isError && (
        <AntAlert
          type="error"
          style={{ marginBottom: 16 }}
          message="周报列表加载失败"
          description={(runs.error as Error)?.message}
        />
      )}

      <Table<ReportRun>
        rowKey="id"
        columns={columns}
        dataSource={runs.data ?? []}
        loading={runs.isLoading}
        pagination={{ pageSize: 20, showSizeChanger: false }}
        locale={{ emptyText: '暂无周报记录，点击右上角手动生成' }}
      />

      <Modal
        title={`周报 ${preview?.period ?? ''}`}
        open={!!preview}
        onCancel={() => setPreview(null)}
        footer={null}
        width={960}
        styles={{ body: { padding: 0 } }}
      >
        {preview?.error ? (
          <Descriptions size="small" style={{ padding: 16 }}>
            <Descriptions.Item label="错误">{preview.error}</Descriptions.Item>
          </Descriptions>
        ) : null}
        {preview ? (
          <iframe
            title={`report-${preview.id}`}
            src={reportsApi.previewUrl(preview.id)}
            style={{ width: '100%', height: '70vh', border: 'none', display: 'block', borderRadius: 8 }}
          />
        ) : null}
      </Modal>
    </div>
  );
}
