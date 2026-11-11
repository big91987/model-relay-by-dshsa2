import { ReloadOutlined } from '@ant-design/icons'
import type { TableProps } from 'antd'
import { Button, Card, Input, Select, Space, Table, Tag, Tooltip, Typography, message } from 'antd'
import dayjs from 'dayjs'
import { useCallback, useEffect, useState } from 'react'
import { api, type LogEntry } from '../api/client'

export default function Logs() {
  const [items, setItems] = useState<LogEntry[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [model, setModel] = useState('')
  const [status, setStatus] = useState('')
  const [loading, setLoading] = useState(false)
  const pageSize = 20

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const result = await api.logs.list({ page, page_size: pageSize, model, status })
      setItems(result.items)
      setTotal(result.total)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '加载日志失败')
    } finally {
      setLoading(false)
    }
  }, [page, model, status])

  useEffect(() => {
    void load()
  }, [load])

  const columns: TableProps<LogEntry>['columns'] = [
    {
      title: '时间',
      dataIndex: 'created_at',
      width: 160,
      render: (value: string) => dayjs(value).format('YYYY-MM-DD HH:mm:ss'),
    },
    { title: '密钥', dataIndex: 'token_name', width: 120, ellipsis: true },
    { title: '渠道', dataIndex: 'channel_name', width: 120, ellipsis: true },
    { title: '模型', dataIndex: 'model', width: 160, ellipsis: true },
    {
      title: '状态',
      dataIndex: 'status_code',
      width: 90,
      render: (code: number) => (
        <Tag color={code >= 200 && code < 300 ? 'green' : 'red'}>{code}</Tag>
      ),
    },
    {
      title: '耗时',
      dataIndex: 'latency_ms',
      width: 90,
      render: (value: number) => `${value} ms`,
    },
    { title: 'Prompt', dataIndex: 'prompt_tokens', width: 90 },
    { title: 'Completion', dataIndex: 'completion_tokens', width: 110 },
    { title: '合计', dataIndex: 'total_tokens', width: 90 },
    {
      title: '流式',
      dataIndex: 'stream',
      width: 80,
      render: (value: boolean) => (value ? <Tag color="blue">SSE</Tag> : <Tag>否</Tag>),
    },
    {
      title: '错误',
      dataIndex: 'error',
      ellipsis: true,
      render: (value: string) =>
        value ? (
          <Tooltip title={value}>
            <Typography.Text type="danger">{value}</Typography.Text>
          </Tooltip>
        ) : (
          <Typography.Text type="secondary">-</Typography.Text>
        ),
    },
  ]

  return (
    <div>
      <div className="page-header">
        <Typography.Title level={4} style={{ margin: 0 }}>
          请求日志
        </Typography.Title>
        <Button icon={<ReloadOutlined />} onClick={() => void load()} loading={loading}>
          刷新
        </Button>
      </div>
      <Card>
        <div className="table-toolbar">
          <Input.Search
            allowClear
            placeholder="按模型筛选"
            style={{ width: 240 }}
            onSearch={(value) => {
              setPage(1)
              setModel(value.trim())
            }}
          />
          <Space>
            <span>状态：</span>
            <Select
              value={status}
              style={{ width: 120 }}
              onChange={(value) => {
                setPage(1)
                setStatus(value)
              }}
              options={[
                { value: '', label: '全部' },
                { value: 'success', label: '成功' },
                { value: 'error', label: '失败' },
              ]}
            />
          </Space>
        </div>
        <Table<LogEntry>
          rowKey="id"
          loading={loading}
          columns={columns}
          dataSource={items}
          scroll={{ x: 1200 }}
          pagination={{
            current: page,
            pageSize,
            total,
            showSizeChanger: false,
            onChange: (next) => setPage(next),
            showTotal: (count) => `共 ${count} 条`,
          }}
          locale={{ emptyText: '暂无日志' }}
        />
      </Card>
    </div>
  )
}
