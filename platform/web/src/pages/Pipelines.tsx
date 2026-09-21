import { useState } from 'react';
import {
  Alert,
  Button,

  Form,
  message,
  Modal,
  Select,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd';
import { Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { pipelinesApi, type PipelineRun } from '../api/pipelines';
import { servicesApi } from '../api/services';

const STATUS_COLOR: Record<string, string> = {
  queued: 'default',
  running: 'processing',
  success: 'success',
  failed: 'error',
};

const STATUS_TEXT: Record<string, string> = {
  queued: '排队中',
  running: '运行中',
  success: '成功',
  failed: '失败',
};

function shortDigest(digest: string) {
  if (!digest) return '-';
  return digest.length > 19 ? `${digest.slice(0, 19)}…` : digest;
}

export default function Pipelines() {
  const queryClient = useQueryClient();
  const [triggerOpen, setTriggerOpen] = useState(false);
  const [form] = Form.useForm();

  const runs = useQuery({
    queryKey: ['pipelines'],
    queryFn: () => pipelinesApi.list({ limit: 50 }),
    refetchInterval: 15000,
  });

  const catalog = useQuery({
    queryKey: ['services-all'],
    queryFn: () => servicesApi.list(),
  });

  const trigger = useMutation({
    mutationFn: (body: Parameters<typeof pipelinesApi.trigger>[0]) =>
      pipelinesApi.trigger(body),
    onSuccess: (created) => {
      message.success(`已触发 ${created.length} 个服务的流水线`);
      setTriggerOpen(false);
      form.resetFields();
      queryClient.invalidateQueries({ queryKey: ['pipelines'] });
    },
    onError: (error: any) => {
      message.error(error?.response?.data?.message || '触发失败');
    },
  });

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 70 },
    {
      title: '服务',
      dataIndex: 'service',
      key: 'service',
      render: (name: string) => (
        <Link to={`/services/${name}`}>
          {name}
        </Link>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      render: (s: string) => (
        <Tag color={STATUS_COLOR[s] ?? 'default'}>{STATUS_TEXT[s] ?? s}</Tag>
      ),
    },
    { title: '构建号', dataIndex: 'jenkinsBuild', key: 'build', width: 90 },
    { title: '触发人', dataIndex: 'triggeredBy', key: 'by' },
    {
      title: '镜像 Digest',
      dataIndex: 'imageDigest',
      key: 'digest',
      render: shortDigest,
    },
    {
      title: '开始时间',
      dataIndex: 'createdAt',
      key: 'createdAt',
      render: (v: string) => new Date(v).toLocaleString('zh-CN', { hour12: false }),
    },
    {
      title: '',
      key: 'actions',
      width: 80,
      render: (_: unknown, run: PipelineRun) => <Link to={`/pipelines/${run.id}`}>详情</Link>,
    },
  ];

  const onFinish = (values: {
    services: string[];
    beforeSha?: string;
    afterSha?: string;
    skipRelease: boolean;
  }) => {
    trigger.mutate({
      services: values.services,
      beforeSha: values.beforeSha,
      afterSha: values.afterSha,
      skipRelease: values.skipRelease ?? false,
    });
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Typography.Title level={4} style={{ marginTop: 0 }}>
          发布与流水线
        </Typography.Title>
        <Button type="primary" onClick={() => setTriggerOpen(true)}>
          触发构建
        </Button>
      </div>

      {runs.isError && (
        <Alert
          type="error"
          style={{ marginBottom: 16 }}
          message="流水线列表加载失败"
          description={(runs.error as Error)?.message}
        />
      )}

      <Table<PipelineRun>
        rowKey="id"
        columns={columns}
        dataSource={runs.data ?? []}
        loading={runs.isLoading}
        pagination={{ pageSize: 20, showSizeChanger: false }}
        locale={{ emptyText: '暂无流水线记录，点击右上角触发第一次构建' }}
        onRow={(run) => ({ onClick: () => window.location.assign(`#/pipelines/${run.id}`) })}
      />

      <Modal
        title="触发流水线"
        open={triggerOpen}
        onCancel={() => setTriggerOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={trigger.isPending}
        okText="触发"
        cancelText="取消"
      >
        <Form form={form} layout="vertical" onFinish={onFinish} initialValues={{ skipRelease: false }}>
          <Form.Item
            name="services"
            label="服务（可多选）"
            rules={[{ required: true, message: '请选择至少一个服务' }]}
          >
            <Select
              mode="multiple"
              placeholder="选择要构建的服务"
              options={(catalog.data ?? []).map((s) => ({ value: s.name, label: s.name }))}
            />
          </Form.Item>
          <Form.Item name="beforeSha" label="Before SHA（可选）">
            <Select
              placeholder="默认空（保守全量构建）"
              allowClear
              mode="tags"
              maxCount={1}
              tokenSeparators={[' ', ',']}
              options={[]}
            />
          </Form.Item>
          <Form.Item name="afterSha" label="After SHA（可选）">
            <Select
              placeholder="默认空"
              allowClear
              mode="tags"
              maxCount={1}
              tokenSeparators={[' ', ',']}
              options={[]}
            />
          </Form.Item>
          <Form.Item
            name="skipRelease"
            label="SKIP_RELEASE（只构建推送，不走发布链）"
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
