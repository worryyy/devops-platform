import { Descriptions, Card, Table, Tag, Typography, Alert, Spin } from 'antd';
import { useParams, Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { servicesApi, type ServiceEnvironment } from '../api/services';

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
