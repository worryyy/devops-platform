import { useState } from 'react';
import { Card, List, Typography } from 'antd';

// uid 对照见 monitoring/dashboards/README.md；新增大盘：改 json → 跑
// sync-dashboards.sh → 这里补一项。
const DASHBOARDS = [
  { uid: 'node-res', slug: 'jie-dian-zi-yuan', title: '节点资源' },
  { uid: 'pod-topn', slug: 'pod-zi-yuan-topn', title: 'Pod TopN' },
  { uid: 'ecampus-sli', slug: 'ecampus-fu-wu-sli', title: '服务 SLI' },
];

export default function Dashboards() {
  const [active, setActive] = useState(DASHBOARDS[0]);

  return (
    <div style={{ display: 'flex', gap: 16, minHeight: 'calc(100vh - 160px)' }}>
      <Card size="small" style={{ width: 200, flexShrink: 0 }} styles={{ body: { padding: 8 } }}>
        <Typography.Text type="secondary" style={{ padding: '8px 12px', display: 'block' }}>
          大盘
        </Typography.Text>
        <List
          size="small"
          dataSource={DASHBOARDS}
          renderItem={(item) => (
            <List.Item
              onClick={() => setActive(item)}
              style={{
                cursor: 'pointer',
                padding: '8px 12px',
                borderRadius: 6,
                background: item.uid === active.uid ? '#e6f4ff' : undefined,
                fontWeight: item.uid === active.uid ? 600 : 400,
              }}
            >
              {item.title}
            </List.Item>
          )}
        />
      </Card>
      <Card
        size="small"
        style={{ flex: 1 }}
        styles={{ body: { padding: 0, height: '100%' } }}
        title={`${active.title}（Grafana 嵌入）`}
      >
        <iframe
          key={active.uid}
          title={active.title}
          src={`/grafana/d/${active.uid}/${active.slug}?kiosk`}
          style={{ width: '100%', height: 'calc(100vh - 200px)', border: 'none', display: 'block' }}
        />
      </Card>
    </div>
  );
}
