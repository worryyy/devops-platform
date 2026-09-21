import type { ReactElement } from 'react';
import { ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import AppLayout from './layouts/AppLayout';
import Login from './pages/Login';
import Services from './pages/Services';
import ServiceDetail from './pages/ServiceDetail';
import Pipelines from './pages/Pipelines';
import PipelineDetail from './pages/PipelineDetail';
import Alerts from './pages/Alerts';
import Logs from './pages/Logs';
import Dashboards from './pages/Dashboards';
import Reports from './pages/Reports';
import { tokenStore } from './api/client';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, refetchOnWindowFocus: false },
  },
});

function RequireAuth({ children }: { children: ReactElement }) {
  if (!tokenStore.get()) {
    return <Navigate to="/login" replace />;
  }
  return children;
}

export default function App() {
  return (
    <ConfigProvider locale={zhCN}>
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route
              path="/"
              element={
                <RequireAuth>
                  <AppLayout />
                </RequireAuth>
              }
            >
              <Route index element={<Navigate to="/services" replace />} />
              <Route path="services" element={<Services />} />
              <Route path="services/:name" element={<ServiceDetail />} />
              <Route path="pipelines" element={<Pipelines />} />
              <Route path="pipelines/:id" element={<PipelineDetail />} />
              <Route path="alerts" element={<Alerts />} />
              <Route path="logs" element={<Logs />} />
              <Route path="dashboards" element={<Dashboards />} />
              <Route path="reports" element={<Reports />} />
            </Route>
          </Routes>
        </BrowserRouter>
      </QueryClientProvider>
    </ConfigProvider>
  );
}
