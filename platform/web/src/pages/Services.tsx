import { useEffect, useState } from 'react';
import { Input, Table, Typography } from 'antd';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { servicesApi, type ServiceSummary } from '../api/services';

export default function Services() {
  const [search, setSearch] = useState('');
  const [debounced, setDebounced] = useState('');

  useEffect(() => {
    const timer = setTimeout(() => setDebounced(search), 300);
    return () => clearTimeout(timer);
  }, [search]);

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['services', debounced],
    queryFn: () => servicesApi.list(debounced || undefined),
  });

  const columns = [
    {
      title: '服务',
      dataIndex: 'name',
      key: 'name',
      render: (name: string) => <Link to={`/services/${name}`}>{name}</Link>,
    },
    { title: '显示名', dataIndex: 'displayName', key: 'displayName' },
    { title: '负责人', dataIndex: 'owner', key: 'owner' },
    { title: '类型', dataIndex: 'kind', key: 'kind' },
    { title: '环境数', dataIndex: 'environmentCount', key: 'environmentCount' },
  ];

  return (
    <div>
      <Typography.Title level={4} style={{ marginTop: 0 }}>
        服务目录
      </Typography.Title>
      <Input.Search
        placeholder="按名称 / 显示名 / 负责人搜索"
        allowClear
        style={{ maxWidth: 320, marginBottom: 16 }}
        onSearch={setSearch}
        onChange={(event) => setSearch(event.target.value)}
      />
      <Table<ServiceSummary>
        rowKey="name"
        columns={columns}
        dataSource={data ?? []}
        loading={isLoading}
        pagination={{ pageSize: 20, showSizeChanger: false }}
        locale={{
          emptyText: isError
            ? `加载失败：${(error as Error)?.message ?? '未知错误'}`
            : '暂无服务，请先执行目录导入',
        }}
      />
    </div>
  );
}
