import { useState } from 'react';
import { BrowserRouter, Route, Routes } from 'react-router-dom';
import { Button } from '@telegram-apps/telegram-ui';
import LoginScreen from './LoginScreen';
import HomePage from './HomePage';
import TeamDetailPage from './TeamDetailPage';
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
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<HomePage />} />
          <Route path="/teams/:name" element={<TeamDetailPage />} />
        </Routes>
      </BrowserRouter>
      <Button onClick={async () => { try { await api.logout(); } finally { setToken(''); setAuthed(false); } }}>
        Выйти
      </Button>
    </div>
  );
}