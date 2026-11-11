import { MinusCircleOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import type { TableProps } from 'antd'
import {
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
  message,
} from 'antd'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { api, type Channel, type ChannelInput } from '../api/client'

interface MappingPair {
  from: string
  to: string
}

interface ChannelFormValues {
  name: string
  base_url: string
  api_key?: string
  models: string[]
  model_mapping?: MappingPair[]
  priority: number
  weight: number
  status: boolean
}

export default function Channels() {
  const [items, setItems] = useState<Channel[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState('')
  const [loading, setLoading] = useState(false)
  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<Channel | null>(null)
  const [saving, setSaving] = useState(false)
  const [testingId, setTestingId] = useState<number | null>(null)
  const [form] = Form.useForm<ChannelFormValues>()
  const pageSize = 10

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const result = await api.channels.list({ page, page_size: pageSize, search })
      setItems(result.items)
      setTotal(result.total)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '加载渠道失败')
    } finally {
      setLoading(false)
    }
  }, [page, search])

  useEffect(() => {
    void load()
  }, [load])

  const modelOptions = useMemo(() => {
    const values = new Set<string>()
    for (const channel of items) {
      for (const model of channel.models) {
        if (model !== '*') {
          values.add(model)
        }
      }
    }
    return Array.from(values)
      .sort()
      .map((value) => ({ value, label: value }))
  }, [items])

  const openCreate = () => {
    setEditing(null)
    form.resetFields()
    form.setFieldsValue({
      name: '',
      base_url: '',
      api_key: '',
      models: [],
      model_mapping: [],
      priority: 0,
      weight: 1,
      status: true,
    })
    setModalOpen(true)
  }

  const openEdit = (channel: Channel) => {
    setEditing(channel)
    form.setFieldsValue({
      name: channel.name,
      base_url: channel.base_url,
      api_key: '',
      models: channel.models,
      model_mapping: Object.entries(channel.model_mapping ?? {}).map(([from, to]) => ({ from, to })),
      priority: channel.priority,
      weight: channel.weight,
      status: channel.status === 1,
    })
    setModalOpen(true)
  }

  const save = async (values: ChannelFormValues) => {
    setSaving(true)
    try {
      const modelMapping: Record<string, string> = {}
      for (const pair of values.model_mapping ?? []) {
        const from = pair?.from?.trim()
        const to = pair?.to?.trim()
        if (from && to) {
          modelMapping[from] = to
        }
      }
      const base: Omit<ChannelInput, 'api_key'> = {
        name: values.name.trim(),
        base_url: values.base_url.trim(),
        models: values.models ?? [],
        model_mapping: modelMapping,
        priority: values.priority ?? 0,
        weight: values.weight ?? 1,
        status: values.status ? 1 : 0,
      }
      if (editing) {
        const apiKey = values.api_key?.trim()
        await api.channels.update(editing.id, apiKey ? { ...base, api_key: apiKey } : base)
        message.success('渠道已更新')
      } else {
        await api.channels.create({ ...base, api_key: (values.api_key ?? '').trim() })
        message.success('渠道已创建')
      }
      setModalOpen(false)
      void load()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存渠道失败')
    } finally {
      setSaving(false)
    }
  }

  const toggleStatus = async (channel: Channel, checked: boolean) => {
    try {
      await api.channels.update(channel.id, { status: checked ? 1 : 0 })
      message.success(checked ? '渠道已启用' : '渠道已停用')
      void load()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '更新状态失败')
    }
  }

  const remove = async (channel: Channel) => {
    try {
      await api.channels.remove(channel.id)
      message.success('渠道已删除')
      void load()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '删除失败')
    }
  }

  const testChannel = async (channel: Channel) => {
    setTestingId(channel.id)
    try {
      const result = await api.channels.test(channel.id)
      if (result.ok) {
        message.success(`连通正常 · ${result.latency_ms}ms · ${result.models.length} 个模型`)
      } else {
        const status = result.status_code ? ` (${result.status_code})` : ''
        message.error(`测试失败${status}: ${result.error || '未知错误'}`)
      }
    } catch (error) {
      message.error(error instanceof Error ? error.message : '测试失败')
    } finally {
      setTestingId(null)
    }
  }

  const columns: TableProps<Channel>['columns'] = [
    { title: '名称', dataIndex: 'name', width: 140, ellipsis: true },
    {
      title: 'Base URL',
      dataIndex: 'base_url',
      ellipsis: true,
      render: (value: string) => <span className="mono">{value}</span>,
    },
    {
      title: '模型',
      dataIndex: 'models',
      width: 240,
      render: (models: string[]) => (
        <Space size={[0, 4]} wrap>
          {models.slice(0, 3).map((model) => (
            <Tag key={model}>{model}</Tag>
          ))}
          {models.length > 3 && <Tag>+{models.length - 3}</Tag>}
        </Space>
      ),
    },
    {
      title: '模型映射',
      dataIndex: 'model_mapping',
      width: 190,
      render: (mapping: Record<string, string>) => {
        const entries = Object.entries(mapping ?? {})
        if (entries.length === 0) {
          return <Typography.Text type="secondary">-</Typography.Text>
        }
        return (
          <Tooltip title={entries.map(([from, to]) => `${from} → ${to}`).join('\n')}>
            <Space size={[0, 4]} wrap>
              {entries.slice(0, 2).map(([from, to]) => (
                <Tag key={from} color="blue">
                  {from}→{to}
                </Tag>
              ))}
              {entries.length > 2 && <Tag>+{entries.length - 2}</Tag>}
            </Space>
          </Tooltip>
        )
      },
    },
    {
      title: '密钥',
      dataIndex: 'api_key_masked',
      width: 130,
      render: (value: string) => <span className="mono">{value}</span>,
    },
    { title: '优先级', dataIndex: 'priority', width: 80 },
    { title: '权重', dataIndex: 'weight', width: 70 },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (status: number, channel) => (
        <Switch
          checked={status === 1}
          checkedChildren="启用"
          unCheckedChildren="停用"
          onChange={(checked) => void toggleStatus(channel, checked)}
        />
      ),
    },
    {
      title: '操作',
      width: 200,
      render: (_, channel) => (
        <Space>
          <Button
            type="link"
            size="small"
            loading={testingId === channel.id}
            onClick={() => void testChannel(channel)}
          >
            测试
          </Button>
          <Button type="link" size="small" onClick={() => openEdit(channel)}>
            编辑
          </Button>
          <Popconfirm title="确定删除该渠道？" onConfirm={() => void remove(channel)}>
            <Button type="link" size="small" danger>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div>
      <div className="page-header">
        <Typography.Title level={4} style={{ margin: 0 }}>
          渠道管理
        </Typography.Title>
        <Space>
          <Input.Search
            allowClear
            placeholder="搜索名称或 Base URL"
            style={{ width: 240 }}
            onSearch={(value) => {
              setPage(1)
              setSearch(value.trim())
            }}
          />
          <Button icon={<ReloadOutlined />} onClick={() => void load()} loading={loading}>
            刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            新建渠道
          </Button>
        </Space>
      </div>

      <Card>
        <Table<Channel>
          rowKey="id"
          loading={loading}
          columns={columns}
          dataSource={items}
          scroll={{ x: 1280 }}
          pagination={{
            current: page,
            pageSize,
            total,
            showSizeChanger: false,
            onChange: (next) => setPage(next),
            showTotal: (count) => `共 ${count} 条`,
          }}
          locale={{ emptyText: '还没有渠道，点击「新建渠道」添加一个上游' }}
        />
      </Card>

      <Modal
        title={editing ? '编辑渠道' : '新建渠道'}
        open={modalOpen}
        confirmLoading={saving}
        okText="保存"
        cancelText="取消"
        width={640}
        onOk={() => form.submit()}
        onCancel={() => setModalOpen(false)}
      >
        <Form<ChannelFormValues> form={form} layout="vertical" onFinish={(values) => void save(values)}>
          <Form.Item
            name="name"
            label="名称"
            rules={[{ required: true, message: '请输入渠道名称' }]}
          >
            <Input placeholder="例如：OpenAI 官方" />
          </Form.Item>
          <Form.Item
            name="base_url"
            label="Base URL"
            rules={[{ required: true, message: '请输入上游 Base URL' }]}
          >
            <Input placeholder="例如：https://api.openai.com" />
          </Form.Item>
          <Form.Item
            name="api_key"
            label="上游 API Key"
            rules={[{ required: !editing, message: '请输入上游 API Key' }]}
          >
            <Input.Password placeholder={editing ? '留空表示不修改' : 'sk-...'} />
          </Form.Item>
          <Form.Item
            name="models"
            label="支持的模型"
            rules={[{ required: true, message: '请至少添加一个模型' }]}
          >
            <Select
              mode="tags"
              placeholder="输入模型名后回车，例如 gpt-4o"
              options={modelOptions}
              tokenSeparators={[',', ' ']}
            />
          </Form.Item>
          <Form.Item label="模型映射（可选，请求模型 → 上游模型）">
            <Form.List name="model_mapping">
              {(fields, { add, remove }) => (
                <>
                  {fields.map(({ key, name }) => (
                    <Space key={key} align="baseline" style={{ display: 'flex', marginBottom: 8 }}>
                      <Form.Item
                        name={[name, 'from']}
                        rules={[{ required: true, message: '请输入请求模型' }]}
                        style={{ marginBottom: 0 }}
                      >
                        <Input placeholder="请求模型" />
                      </Form.Item>
                      <span>→</span>
                      <Form.Item
                        name={[name, 'to']}
                        rules={[{ required: true, message: '请输入上游模型' }]}
                        style={{ marginBottom: 0 }}
                      >
                        <Input placeholder="上游模型" />
                      </Form.Item>
                      <MinusCircleOutlined onClick={() => remove(name)} />
                    </Space>
                  ))}
                  <Button
                    type="dashed"
                    block
                    icon={<PlusOutlined />}
                    onClick={() => add({ from: '', to: '' })}
                  >
                    添加映射
                  </Button>
                </>
              )}
            </Form.List>
          </Form.Item>
          <Space size="large" align="start" style={{ display: 'flex' }}>
            <Form.Item name="priority" label="优先级（越大越优先）" style={{ flex: 1 }}>
              <InputNumber min={0} max={100} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="weight" label="权重" style={{ flex: 1 }}>
              <InputNumber min={1} max={100} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="status" label="状态" valuePropName="checked">
              <Switch checkedChildren="启用" unCheckedChildren="停用" />
            </Form.Item>
          </Space>
        </Form>
      </Modal>
    </div>
  )
}
