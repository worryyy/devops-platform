import { useState } from 'react';
import {
  Alert,
  Button,
  Card,
  Descriptions,
  message,
  Modal,
  Space,
  Spin,
  Steps,
  Tag,
  Typography,
} from 'antd';
import { Link, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { pipelinesApi, type StageEntry } from '../api/pipelines';

const STEP_ICON: Record<string, 'finish' | 'process' | 'error' | 'wait'> = {
  success: 'finish',
  running: 'process',
  failed: 'error',
  queued: 'wait',
  skipped: 'wait',
};

function formatDuration(ms: number) {
  if (!ms) return '';
  if (ms < 1000) return `${ms}ms`;
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  return `${Math.floor(s / 60)}m${s % 60}s`;
}

export default function PipelineDetail() {
  const { id } = useParams<{ id: string }>();
  const queryClient = useQueryClient();
  const [reverting, setReverting] = useState(false);

  const run = useQuery({
    queryKey: ['pipeline', id],
    queryFn: () => pipelinesApi.detail(Number(id)),
    enabled: Boolean(id),
    refetchInterval: (query) =>
      query.state.data?.status === 'running' || query.state.data?.status === 'queued'
        ? 10000
        : false,
  });

  const argo = useQuery({
    queryKey: ['pipeline-argocd', id],
    queryFn: () => pipelinesApi.argocd(Number(id)),
    enabled: Boolean(id),
    refetchInterval: 30000,
    retry: false,
  });

  const revert = useMutation({
    mutationFn: () => pipelinesApi.revert(Number(id)),
    onSuccess: (result) => {
      message.success(`Revert PR 已创建：#${result.number}`);
      queryClient.invalidateQueries({ queryKey: ['pipeline', id] });
      window.open(result.prUrl, '_blank');
    },
    onError: (error: any) => {
      message.error(error?.response?.data?.message || '创建 revert PR 失败');
    },
  });

  if (run.isLoading) return <Spin size="large" style={{ display: 'block', marginTop: 80 }} />;
  if (run.isError) {
    return <Alert type="error" message="加载失败" description={(run.error as Error)?.message} />;
  }
  const data = run.data;
  if (!data) return null;

  const stages: StageEntry[] = data.stages ?? [];

  return (
    <div>
      <Typography.Title level={4} style={{ marginTop: 0 }}>
        <Link to="/pipelines">发布与流水线</Link> / #{data.id} {data.service}
      </Typography.Title>

      <Card title="基本信息" style={{ marginBottom: 16 }}>
        <Descriptions column={3} size="small">
          <Descriptions.Item label="状态">
            <Tag
              color={
                data.status === 'success'
                  ? 'success'
                  : data.status === 'failed'
                    ? 'error'
                    : data.status === 'running'
                      ? 'processing'
                      : 'default'
              }
            >
              {data.status}
            </Tag>
          </Descriptions.Item>
          <Descriptions.Item label="Jenkins 构建">
            {data.jenkinsBuild ? (
              <span>#{data.jenkinsBuild}</span>
            ) : (
              '-'
            )}
          </Descriptions.Item>
          <Descriptions.Item label="触发人">{data.triggeredBy}</Descriptions.Item>
          <Descriptions.Item label="Git Revision">
            <code style={{ fontSize: 12 }}>{data.gitRevision?.slice(0, 12) || '-'}</code>
          </Descriptions.Item>
          <Descriptions.Item label="镜像 Digest">
            <code style={{ fontSize: 12 }}>{data.imageDigest?.slice(0, 25) || '-'}</code>
          </Descriptions.Item>
          <Descriptions.Item label="创建时间">
            {new Date(data.createdAt).toLocaleString('zh-CN', { hour12: false })}
          </Descriptions.Item>
        </Descriptions>
        {data.revertPrUrl && (
          <Alert
            style={{ marginTop: 12 }}
            type="info"
            message={
              <>
                Revert PR 已创建：
                <a href={data.revertPrUrl} target="_blank" rel="noreferrer">
                  {data.revertPrUrl}
                </a>
              </>
            }
          />
        )}
        {data.status === 'failed' && !data.revertPrUrl && (
          <Space style={{ marginTop: 12 }}>
            <Button
              danger
              loading={reverting}
              onClick={() => {
                setReverting(true);
                Modal.confirm({
                  title: `回退 ${data.service} 的这次发布？`,
                  content: '将创建 revert PR（分支 revert/<service>/<build>），合并后集群回到上一版。',
                  okText: '创建 Revert PR',
                  cancelText: '取消',
                  onOk: () => revert.mutateAsync(),
                  onCancel: () => setReverting(false),
                });
              }}
            >
              一键 Revert
            </Button>
          </Space>
        )}
      </Card>

      <Card title="Stage 时间线" style={{ marginBottom: 16 }}>
        {stages.length === 0 ? (
          <Typography.Text type="secondary">等待 webhook 推送阶段进度…</Typography.Text>
        ) : (
          <Steps
            direction="vertical"
            size="small"
            items={stages.map((st) => ({
              title: st.name,
              status: STEP_ICON[st.status] ?? 'wait',
              description: (
                <>
                  <Tag style={{ marginRight: 8 }}>{st.status}</Tag>
                  {st.durationMs ? formatDuration(st.durationMs) : ''}
                </>
              ),
            }))}
          />
        )}
      </Card>

      <Card title="GitOps 同步状态">
        {argo.isError ? (
          <Typography.Text type="secondary">
            Argo CD 状态不可用（{argo.error?.message ?? '未知错误'}）
          </Typography.Text>
        ) : argo.data ? (
          <Descriptions column={4} size="small">
            <Descriptions.Item label="Application">{argo.data.name}</Descriptions.Item>
            <Descriptions.Item label="Sync">
              <Tag color={argo.data.syncStatus === 'Synced' ? 'success' : 'warning'}>
                {argo.data.syncStatus}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="Health">
              <Tag color={argo.data.health === 'Healthy' ? 'success' : 'warning'}>
                {argo.data.health}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="Revision">
              <code style={{ fontSize: 12 }}>{argo.data.syncVersion?.slice(0, 12) || '-'}</code>
            </Descriptions.Item>
          </Descriptions>
        ) : (
          <Spin size="small" />
        )}
      </Card>
    </div>
  );
}
