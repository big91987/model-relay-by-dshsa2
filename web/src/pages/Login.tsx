import { LockOutlined, UserOutlined } from '@ant-design/icons'
import { Button, Card, Form, Input, Typography, message } from 'antd'
import { useState } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { api, getToken, setToken } from '../api/client'

interface LoginValues {
  username: string
  password: string
}

export default function Login() {
  const navigate = useNavigate()
  const [loading, setLoading] = useState(false)

  if (getToken()) {
    return <Navigate to="/" replace />
  }

  const onFinish = async (values: LoginValues) => {
    setLoading(true)
    try {
      const result = await api.login(values.username, values.password)
      setToken(result.token)
      message.success('登录成功')
      navigate('/', { replace: true })
    } catch (error) {
      message.error(error instanceof Error ? error.message : '登录失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-page">
      <Card className="login-card">
        <Typography.Title level={3} className="login-title">
          Model Gateway
        </Typography.Title>
        <Typography.Paragraph className="login-subtitle">模型网关管理台</Typography.Paragraph>
        <Form
          layout="vertical"
          onFinish={onFinish}
          initialValues={{ username: 'admin' }}
        >
          <Form.Item
            name="username"
            label="用户名"
            rules={[{ required: true, message: '请输入用户名' }]}
          >
            <Input prefix={<UserOutlined />} autoComplete="username" />
          </Form.Item>
          <Form.Item
            name="password"
            label="密码"
            rules={[{ required: true, message: '请输入密码' }]}
          >
            <Input.Password prefix={<LockOutlined />} autoComplete="current-password" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block loading={loading}>
            登录
          </Button>
        </Form>
        <Typography.Paragraph type="secondary" style={{ marginTop: 16, marginBottom: 0, textAlign: 'center' }}>
          默认账号 admin / admin123
        </Typography.Paragraph>
      </Card>
    </div>
  )
}
