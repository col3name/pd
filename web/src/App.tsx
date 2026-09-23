import { useState } from 'react';
import { Button, Cell, List, Section } from '@telegram-apps/telegram-ui';
import LoginScreen from './LoginScreen';
import TeamsTab from './TeamsTab';
import { getToken, setToken, api } from './api';

export default function App() {
  const [authed, setAuthed] = useState(() => getToken() !== '');

  if (!authed) {
    return (
      <div style={{ maxWidth: 720, margin: '0 auto', padding: 16 }}>
        <LoginScreen onLogin={() => setAuthed(true)} />
      </div>
    );
  }

  return (
    <div style={{ maxWidth: 720, margin: '0 auto', padding: 16 }}>
      <Section header="PII Gateway — Админка">
        <List>
          <Cell subtitle="команды-потребители">Команды</Cell>
        </List>
      </Section>
      <TeamsTab />
      <Button onClick={async () => { try { await api.logout(); } finally { setToken(''); setAuthed(false); } }}>
        Выйти
      </Button>
    </div>
  );
}