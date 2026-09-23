import { useState } from 'react';
import { Button, Input, Section } from '@telegram-apps/telegram-ui';
import { api, setToken } from './api';

export default function LoginScreen({ onLogin }: { onLogin: () => void }) {
  const [login, setLogin] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');

  const submit = async () => {
    try {
      const res = await api.login(login, password);
      setToken(res.token);
      onLogin();
    } catch (e) {
      setError(String(e));
    }
  };

  return (
    <Section header="Вход в админку">
      <Input title="Логин" value={login} onChange={(e) => setLogin(e.target.value)} placeholder="admin" />
      <Input title="Пароль" type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="••••••" />
      <Button onClick={submit}>Войти</Button>
      {error && <div style={{ color: 'red' }}>{error}</div>}
    </Section>
  );
}