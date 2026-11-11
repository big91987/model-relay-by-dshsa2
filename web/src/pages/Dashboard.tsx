import { ReloadOutlined } from '@ant-design/icons'
import type { TableProps } from 'antd'
import { Button, Card, Col, Row, Statistic, Table, Tag, Typography, message } from 'antd'
import dayjs from 'dayjs'
import { useCallback, useEffect, useState } from 'react'
import { api, type DashboardData, type LogEntry } from '../api/client'

const logColumns: TableProps<LogEntry>['columns'] = [
  {
    title: '时间',
    dataIndex: 'created_at',
    width: 160,
    render: (value: string) => dayjs(value).format('YYYY-MM-DD HH:mm:ss'),
  },
  { title: '模型', dataIndex: 'model', ellipsis: true },
  { title: '渠道', dataIndex: 'channel_name', width: 120, ellipsis: true },
  {
    title: '状态',
    dataIndex: 'status_code',
    width: 90,
    render: (code: number) => (
      <Tag color={code >= 200 && code < 300 ? 'green' : 'red'}>{code}</Tag>
    ),
  },
  { title: 'Token 用量', dataIndex: 'total_tokens', width: 110 },
  {
    title: '耗时',
    dataIndex: 'latency_ms',
    width: 90,
    render: (value: number) => `${value} ms`,
  },
]

export default function Dashboard() {
  const [data, setData] = useState<DashboardData | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setData(await api.dashboard())
    } catch (error) {
      message.error(error instanceof Error ? error.message : '加载概览失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const daily = data?.daily ?? []
  const maxRequests = Math.max(1, ...daily.map((point) => point.requests))

  return (
    <div>
      <div className="page-header">
        <Typography.Title level={4} style={{ margin: 0 }}>
          概览
        </Typography.Title>
        <Button icon={<ReloadOutlined />} onClick={() => void load()} loading={loading}>
          刷新
        </Button>
      </div>

      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} lg={6}>
          <Card className="stat-card">
            <Statistic title="渠道" value={data?.channels.total ?? 0} suffix={`/ 启用 ${data?.channels.enabled ?? 0}`} />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card className="stat-card">
            <Statistic title="密钥" value={data?.tokens.total ?? 0} suffix={`/ 启用 ${data?.tokens.enabled ?? 0}`} />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card className="stat-card">
            <Statistic title="请求总数" value={data?.requests.total ?? 0} suffix={`/ 今日 ${data?.requests.today ?? 0}`} />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card className="stat-card">
            <Statistic
              title="Token 用量"
              value={data?.usage.total_tokens ?? 0}
              suffix={`/ 今日 ${data?.usage.today_tokens ?? 0}`}
            />
          </Card>
        </Col>
      </Row>

      <Card title="近 7 日请求" style={{ marginTop: 16 }}>
        <div className="trend">
          {daily.map((point) => (
            <div className="trend-item" key={point.date}>
              <div className="trend-bar-wrap">
                <div
                  className="trend-bar"
                  style={{ height: `${Math.max(4, Math.round((point.requests / maxRequests) * 120))}px` }}
                  title={`${point.date}: ${point.requests} 次请求 / ${point.tokens} tokens`}
                />
              </div>
              <div className="trend-count">{point.requests}</div>
              <div className="trend-label">{dayjs(point.date).format('MM-DD')}</div>
            </div>
          ))}
        </div>
      </Card>

      <Card
        title="最近请求"
        style={{ marginTop: 16 }}
        extra={<Typography.Text type="secondary">最近 10 条</Typography.Text>}
      >
        <Table<LogEntry>
          rowKey="id"
          size="small"
          loading={loading}
          columns={logColumns}
          dataSource={data?.recent_logs ?? []}
          pagination={false}
          locale={{ emptyText: '暂无请求日志' }}
        />
      </Card>
    </div>
  )
}
