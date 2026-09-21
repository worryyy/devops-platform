import { Descriptions, Card, Table, Tag, Typography, Alert, Spin } from 'antd';
import { useParams, Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { servicesApi, type ServiceEnvironment } from '../api/services';
import { pipelinesApi, type PipelineRun } from '../api/pipelines';

export default function ServiceDetail() {
  const { name } = useParams<{ name: string }>();

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['service', name],
    queryFn: () => servicesApi.detail(name!),
    enabled: Boolean(name),
  });

  if (isLoading) return <Spin size="large" style={{ display: 'block', marginTop: 80 }} />;
  if (isError) {
    return <Alert type="error" message="加载失败" description={(error as Error)?.message} />;
  }
  if (!data) return null;

  const dev = data.environments[0];

  const envColumns = [
    { title: '环境', dataIndex: 'name', key: 'name', render: (v: string) => <Tag>{v}</Tag> },
    { title: '命名空间', dataIndex: ['kubernetes', 'namespace'], key: 'namespace' },
    { title: '默认分支', dataIndex: ['branchPolicy', 'defaultBranch'], key: 'branch' },
    { title: '工作负载', dataIndex: ['kubernetes', 'workload'], key: 'workload' },
    { title: '健康检查', dataIndex: ['health', 'healthPath'], key: 'health' },
  ];

  return (
    <div>
      <Typography.Title level={4} style={{ marginTop: 0 }}>
        <Link to="/services">服务目录</Link> / {data.name}
      </Typography.Title>

      <Card title="基本信息" style={{ marginBottom: 16 }}>
        <Descriptions column={3}>
          <Descriptions.Item label="显示名">{data.displayName || '-'}</Descriptions.Item>
          <Descriptions.Item label="负责人">{data.owner || '-'}</Descriptions.Item>
          <Descriptions.Item label="类型">{data.kind || '-'}</Descriptions.Item>
        </Descriptions>
      </Card>

      <Card title="SLI 策略" style={{ marginBottom: 16 }}>
        <Descriptions column={3}>
          <Descriptions.Item label="请求路由">
            <code>{data.sli?.requestRouteRegex || '-'}</code>
          </Descriptions.Item>
          <Descriptions.Item label="操作路由">
            <code>{data.sli?.operationRouteRegex || '-'}</code>
          </Descriptions.Item>
          <Descriptions.Item label="P95 上限（秒）">
            {data.sli?.maxP95Seconds ?? '-'}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      {dev && (
        <Card title="环境元数据（dev）" style={{ marginBottom: 16 }}>
          <Descriptions column={2} size="small">
            <Descriptions.Item label="Git 仓库">
              <code style={{ fontSize: 12 }}>{dev.git.repo}</code>
            </Descriptions.Item>
            <Descriptions.Item label="Values 文件">
              <code style={{ fontSize: 12 }}>{dev.git.valuesFile}</code>
            </Descriptions.Item>
            <Descriptions.Item label="镜像仓库">
              <code style={{ fontSize: 12 }}>{dev.image.repository}</code>
            </Descriptions.Item>
            <Descriptions.Item label="Tag 策略">{dev.image.tagPolicy}</Descriptions.Item>
            <Descriptions.Item label="Jenkins Job">{dev.jenkins.jobName}</Descriptions.Item>
            <Descriptions.Item label="Argo CD App">{dev.argocd.application}</Descriptions.Item>
          </Descriptions>
        </Card>
      )}

      <Card title="最近发布" style={{ marginBottom: 16 }}>
        <RecentReleases name={data.name} />
      </Card>

      <Card title="全部环境">
        <Table<ServiceEnvironment>
          rowKey="name"
          columns={envColumns}
          dataSource={data.environments}
          pagination={false}
          size="small"
        />
      </Card>
    </div>
  );
}


function RecentReleases({ name }: { name: string }) {
  const { data, isLoading } = useQuery({
    queryKey: ['service-pipelines', name],
    queryFn: () => pipelinesApi.list({ service: name, limit: 5 }),
  });
  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 70 },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      render: (s: string) => (
        <Tag color={s === 'success' ? 'success' : s === 'failed' ? 'error' : 'processing'}>{s}</Tag>
      ),
    },
    { title: '构建号', dataIndex: 'jenkinsBuild', key: 'build', width: 90 },
    { title: '触发人', dataIndex: 'triggeredBy', key: 'by' },
    {
      title: '时间',
      dataIndex: 'createdAt',
      key: 'createdAt',
      render: (v: string) => new Date(v).toLocaleString('zh-CN', { hour12: false }),
    },
    {
      title: '',
      key: 'detail',
      width: 70,
      render: (_: unknown, run: PipelineRun) => <Link to={`/pipelines/${run.id}`}>详情</Link>,
    },
  ];
  return (
    <Table<PipelineRun>
      rowKey="id"
      size="small"
      columns={columns}
      dataSource={data ?? []}
      loading={isLoading}
      pagination={false}
      locale={{ emptyText: '暂无发布记录' }}
    />
  );
}
