import { Button, Card, Form, Input, message } from 'antd';
import { useNavigate } from 'react-router-dom';
import { authApi } from '../api/services';
import { tokenStore } from '../api/client';

export default function Login() {
  const navigate = useNavigate();

  const onFinish = async (values: { username: string; password: string }) => {
    try {
      const result = await authApi.login(values.username, values.password);
      tokenStore.set(result.token);
      navigate('/');
    } catch (error: any) {
      const detail = error?.response?.data?.message;
      message.error(detail || '登录失败，请检查用户名密码');
    }
  };

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: '#f5f5f5',
      }}
    >
      <Card title="DevOps 运维平台" style={{ width: 360 }}>
        <Form layout="vertical" onFinish={onFinish} autoComplete="off">
          <Form.Item
            name="username"
            label="用户名"
            rules={[{ required: true, message: '请输入用户名' }]}
          >
            <Input placeholder="用户名" />
          </Form.Item>
          <Form.Item
            name="password"
            label="密码"
            rules={[{ required: true, message: '请输入密码' }]}
          >
            <Input.Password placeholder="密码" />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" block>
              登录
            </Button>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
}
