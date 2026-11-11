import {
  ApiOutlined,
  AppstoreOutlined,
  FileTextOutlined,
  KeyOutlined,
  LockOutlined,
  LogoutOutlined,
} from '@ant-design/icons'
import type { MenuProps } from 'antd'
import { Button, Form, Input, Layout, Menu, Modal, Space, Typography, message } from 'antd'
import { useEffect, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { api, clearToken } from '../api/client'

const { Header, Sider, Content } = Layout

const items: MenuProps['items'] = [
  { key: '/', icon: <AppstoreOutlined />, label: '概览' },
  { key: '/channels', icon: <ApiOutlined />, label: '渠道' },
  { key: '/tokens', icon: <KeyOutlined />, label: '密钥' },
  { key: '/logs', icon: <FileTextOutlined />, label: '日志' },
]

function selectedKey(pathname: string): string {
  if (pathname.startsWith('/channels')) return '/channels'
  if (pathname.startsWith('/tokens')) return '/tokens'
  if (pathname.startsWith('/logs')) return '/logs'
  return '/'
}

interface PasswordValues {
  old_password: string
  new_password: string
  confirm: string
}

export default function AppLayout() {
  const navigate = useNavigate()
  const location = useLocation()
  const [username, setUsername] = useState('admin')
  const [passwordOpen, setPasswordOpen] = useState(false)
  const [passwordSaving, setPasswordSaving] = useState(false)
  const [passwordForm] = Form.useForm<PasswordValues>()

  useEffect(() => {
    api
      .me()
      .then((data) => setUsername(data.username))
      .catch(() => {
        // The API client redirects on 401; keep the default label otherwise.
      })
  }, [])

  const logout = () => {
    clearToken()
    navigate('/login', { replace: true })
  }

  const openPassword = () => {
    passwordForm.resetFields()
    setPasswordOpen(true)
  }

  const submitPassword = async (values: PasswordValues) => {
    setPasswordSaving(true)
    try {
      await api.changePassword(values.old_password, values.new_password)
      message.success('密码已修改')
      setPasswordOpen(false)
      passwordForm.resetFields()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '修改密码失败')
    } finally {
      setPasswordSaving(false)
    }
  }

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider width={216} theme="dark">
        <div className="brand">
          <ApiOutlined />
          <span>Model Gateway</span>
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[selectedKey(location.pathname)]}
          items={items}
          onClick={({ key }) => navigate(key)}
        />
      </Sider>
      <Layout>
        <Header className="app-header">
          <Typography.Text type="secondary">管理员：{username}</Typography.Text>
          <Space>
            <Button icon={<LockOutlined />} onClick={openPassword}>
              修改密码
            </Button>
            <Button icon={<LogoutOutlined />} onClick={logout}>
              退出登录
            </Button>
          </Space>
        </Header>
        <Content className="app-content">
          <Outlet />
        </Content>
      </Layout>

      <Modal
        title="修改管理员密码"
        open={passwordOpen}
        confirmLoading={passwordSaving}
        okText="保存"
        cancelText="取消"
        onOk={() => passwordForm.submit()}
        onCancel={() => setPasswordOpen(false)}
      >
        <Form<PasswordValues>
          form={passwordForm}
          layout="vertical"
          onFinish={(values) => void submitPassword(values)}
        >
          <Form.Item
            name="old_password"
            label="当前密码"
            rules={[{ required: true, message: '请输入当前密码' }]}
          >
            <Input.Password autoComplete="current-password" />
          </Form.Item>
          <Form.Item
            name="new_password"
            label="新密码"
            rules={[
              { required: true, message: '请输入新密码' },
              { min: 6, message: '新密码至少 6 位' },
            ]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            name="confirm"
            label="确认新密码"
            dependencies={['new_password']}
            rules={[
              { required: true, message: '请再次输入新密码' },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  if (!value || getFieldValue('new_password') === value) {
                    return Promise.resolve()
                  }
                  return Promise.reject(new Error('两次输入的密码不一致'))
                },
              }),
            ]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
        </Form>
      </Modal>
    </Layout>
  )
}
