import { Layout, Menu, Button } from 'antd';
import { Outlet, useLocation, useNavigate } from 'react-router-dom';
import { tokenStore } from '../api/client';

const { Sider, Header, Content } = Layout;

const NAV_ITEMS = [
  { key: '/services', label: '服务目录' },
];

export default function AppLayout() {
  const navigate = useNavigate();
  const location = useLocation();

  const selectedKey =
    NAV_ITEMS.find((item) => location.pathname.startsWith(item.key))?.key ?? '/services';

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider theme="dark" width={200}>
        <div
          style={{
            color: '#fff',
            padding: '16px',
            fontSize: 16,
            fontWeight: 600,
            whiteSpace: 'nowrap',
          }}
        >
          DevOps 平台
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[selectedKey]}
          items={NAV_ITEMS}
          onClick={({ key }) => navigate(key)}
        />
      </Sider>
      <Layout>
        <Header
          style={{
            background: '#fff',
            display: 'flex',
            justifyContent: 'flex-end',
            alignItems: 'center',
            paddingInline: 24,
          }}
        >
          <Button
            type="text"
            onClick={() => {
              tokenStore.clear();
              navigate('/login');
            }}
          >
            退出登录
          </Button>
        </Header>
        <Content style={{ padding: 24 }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}
