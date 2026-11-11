import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import type { TableProps } from 'antd'
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
  message,
} from 'antd'
import dayjs from 'dayjs'
import { useCallback, useEffect, useState } from 'react'
import { api, type Token } from '../api/client'

interface TokenFormValues {
  name: string
  quota: number
}

export default function Tokens() {
  const [items, setItems] = useState<Token[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<Token | null>(null)
  const [createdKey, setCreatedKey] = useState('')
  const [saving, setSaving] = useState(false)
  const [createForm] = Form.useForm<TokenFormValues>()
  const [editForm] = Form.useForm<TokenFormValues>()
  const pageSize = 10

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const result = await api.tokens.list({ page, page_size: pageSize })
      setItems(result.items)
      setTotal(result.total)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '加载密钥失败')
    } finally {
      setLoading(false)
    }
  }, [page])

  useEffect(() => {
    void load()
  }, [load])

  const openCreate = () => {
    createForm.resetFields()
    createForm.setFieldsValue({ name: '', quota: 0 })
    setCreateOpen(true)
  }

  const submitCreate = async (values: TokenFormValues) => {
    setSaving(true)
    try {
      const token = await api.tokens.create({ name: values.name.trim(), quota: values.quota ?? 0 })
      setCreateOpen(false)
      setCreatedKey(token.key ?? '')
      message.success('密钥已创建')
      void load()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '创建密钥失败')
    } finally {
      setSaving(false)
    }
  }

  const openEdit = (token: Token) => {
    setEditTarget(token)
    editForm.setFieldsValue({ name: token.name, quota: token.quota })
  }

  const submitEdit = async (values: TokenFormValues) => {
    if (!editTarget) {
      return
    }
    setSaving(true)
    try {
      await api.tokens.update(editTarget.id, { name: values.name.trim(), quota: values.quota ?? 0 })
      setEditTarget(null)
      message.success('密钥已更新')
      void load()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '更新密钥失败')
    } finally {
      setSaving(false)
    }
  }

  const toggleStatus = async (token: Token, checked: boolean) => {
    try {
      await api.tokens.update(token.id, { status: checked ? 1 : 0 })
      message.success(checked ? '密钥已启用' : '密钥已停用')
      void load()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '更新状态失败')
    }
  }

  const remove = async (token: Token) => {
    try {
      await api.tokens.remove(token.id)
      message.success('密钥已删除')
      void load()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '删除失败')
    }
  }

  const copyKey = async () => {
    try {
      await navigator.clipboard.writeText(createdKey)
      message.success('已复制到剪贴板')
    } catch {
      message.error('复制失败，请手动选择文本')
    }
  }

  const columns: TableProps<Token>['columns'] = [
    { title: '名称', dataIndex: 'name', width: 140, ellipsis: true },
    {
      title: 'Key',
      dataIndex: 'key_prefix',
      width: 150,
      render: (value: string) => <span className="mono">{value}…</span>,
    },
    {
      title: '额度',
      dataIndex: 'quota',
      width: 100,
      render: (value: number) => (value === 0 ? <Tag>不限</Tag> : value.toLocaleString()),
    },
    {
      title: '已用',
      dataIndex: 'used_quota',
      width: 100,
      render: (value: number) => value.toLocaleString(),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (status: number, token) => (
        <Switch
          checked={status === 1}
          checkedChildren="启用"
          unCheckedChildren="停用"
          onChange={(checked) => void toggleStatus(token, checked)}
        />
      ),
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      width: 160,
      render: (value: string) => dayjs(value).format('YYYY-MM-DD HH:mm:ss'),
    },
    {
      title: '最后使用',
      dataIndex: 'last_used_at',
      width: 160,
      render: (value: string) =>
        value ? dayjs(value).format('YYYY-MM-DD HH:mm:ss') : <Typography.Text type="secondary">-</Typography.Text>,
    },
    {
      title: '操作',
      width: 140,
      render: (_, token) => (
        <Space>
          <Button type="link" size="small" onClick={() => openEdit(token)}>
            编辑
          </Button>
          <Popconfirm title="确定删除该密钥？" onConfirm={() => void remove(token)}>
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
          密钥管理
        </Typography.Title>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={() => void load()} loading={loading}>
            刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            新建密钥
          </Button>
        </Space>
      </div>

      <Card>
        <Table<Token>
          rowKey="id"
          loading={loading}
          columns={columns}
          dataSource={items}
          scroll={{ x: 1100 }}
          pagination={{
            current: page,
            pageSize,
            total,
            showSizeChanger: false,
            onChange: (next) => setPage(next),
            showTotal: (count) => `共 ${count} 条`,
          }}
          locale={{ emptyText: '还没有密钥，点击「新建密钥」创建' }}
        />
      </Card>

      <Modal
        title="新建密钥"
        open={createOpen}
        confirmLoading={saving}
        okText="创建"
        cancelText="取消"
        onOk={() => createForm.submit()}
        onCancel={() => setCreateOpen(false)}
      >
        <Form<TokenFormValues> form={createForm} layout="vertical" onFinish={(values) => void submitCreate(values)}>
          <Form.Item
            name="name"
            label="名称"
            rules={[{ required: true, message: '请输入密钥名称' }]}
          >
            <Input placeholder="例如：本地开发" />
          </Form.Item>
          <Form.Item name="quota" label="额度（总 Token 数，0 表示不限）">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="编辑密钥"
        open={editTarget !== null}
        confirmLoading={saving}
        okText="保存"
        cancelText="取消"
        onOk={() => editForm.submit()}
        onCancel={() => setEditTarget(null)}
      >
        <Form<TokenFormValues> form={editForm} layout="vertical" onFinish={(values) => void submitEdit(values)}>
          <Form.Item
            name="name"
            label="名称"
            rules={[{ required: true, message: '请输入密钥名称' }]}
          >
            <Input />
          </Form.Item>
          <Form.Item name="quota" label="额度（总 Token 数，0 表示不限）">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="密钥创建成功"
        open={createdKey !== ''}
        maskClosable={false}
        onCancel={() => setCreatedKey('')}
        footer={
          <Button type="primary" onClick={() => setCreatedKey('')}>
            我已保存
          </Button>
        }
      >
        <Alert
          type="warning"
          showIcon
          message="完整密钥只显示一次，请立即复制并妥善保存。"
          style={{ marginBottom: 12 }}
        />
        <Input.TextArea value={createdKey} readOnly autoSize style={{ fontFamily: 'monospace' }} />
        <Button style={{ marginTop: 12 }} onClick={() => void copyKey()}>
          复制密钥
        </Button>
      </Modal>
    </div>
  )
}
