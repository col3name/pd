import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { Button, Cell, Input, List, Section } from '@telegram-apps/telegram-ui';
import { api, getToken, type SystemInfo } from './api';

export default function HomePage() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { data: teams, isLoading } = useQuery<SystemInfo[]>({ queryKey: ['systems'], queryFn: api.listSystems });
  const [name, setName] = useState('');
  const [toast, setToast] = useState('');

  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(''), 2000);
    return () => clearTimeout(t);
  }, [toast]);

  const copy = (v: string) => {
    navigator.clipboard?.writeText(v);
    setToast('Скопировано в буфер обмена');
  };

  const downloadLogs = async () => {
    try {
      const res = await fetch(api.logsUrl(), {
        headers: { Authorization: `Bearer ${getToken()}` },
      });
      if (!res.ok) throw new Error(`${res.status}`);
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'pii-gateway.log';
      a.click();
      URL.revokeObjectURL(url);
      setToast('Логи скачаны');
    } catch (e) {
      setToast(`Ошибка скачивания: ${String(e)}`);
    }
  };

  const invalidate = () => qc.invalidateQueries({ queryKey: ['systems'] });

  const create = useMutation({
    mutationFn: (n: string) => api.createSystem({ name: n }),
    onSuccess: () => { setName(''); invalidate(); },
  });
  const remove = useMutation({ mutationFn: (n: string) => api.deleteSystem(n), onSuccess: invalidate });

  if (isLoading) return <div>Загрузка…</div>;

  return (
    <Section header="Команды">
      <List>
        {(teams ?? []).map((t) => (
          <Cell
            key={t.name}
            subtitle={`${t.enabled ? 'включена' : 'отключена'} · режим: ${t.masking || 'глобальный'} · типов ПД: ${(t.pii ?? []).length === 0 ? 'все' : (t.pii ?? []).length}`}
            onClick={() => navigate(`/teams/${encodeURIComponent(t.name)}`)}
          >
            {t.name}
          </Cell>
        ))}
      </List>

      <Section header="Новая команда">
        <Input title="Имя" value={name} onChange={(e) => setName(e.target.value)} placeholder="chat" />
        <Button onClick={() => create.mutate(name)} disabled={!name || create.isPending}>Создать</Button>
      </Section>

      <Section header="Примеры запросов">
        <Cell subtitle="curl — маскирование (новый payload_id)">
          <pre style={{ fontSize: 11, whiteSpace: 'pre-wrap' }}>{`curl -s -X POST http://5.42.118.103:5173/process \\
  -H 'Content-Type: application/json' \\
  -d '{"payload":"паспорт 4509 123456","payload_id":"demo1"}'`}</pre>
          <Button size="s" onClick={() => copy(`curl -s -X POST http://5.42.118.103:5173/process \\
  -H 'Content-Type: application/json' \\
  -d '{"payload":"паспорт 4509 123456","payload_id":"demo1"}'`)}>Копировать</Button>
        </Cell>
        <Cell subtitle="curl — демаскирование (тот же payload_id, payload = маска)">
          <pre style={{ fontSize: 11, whiteSpace: 'pre-wrap' }}>{`curl -s -X POST http://5.42.118.103:5173/process \\
  -H 'Content-Type: application/json' \\
  -d '{"payload":"паспорт [ПАСПОРТ]","payload_id":"demo1"}'`}</pre>
          <Button size="s" onClick={() => copy(`curl -s -X POST http://5.42.118.103:5173/process \\
  -H 'Content-Type: application/json' \\
  -d '{"payload":"паспорт [ПАСПОРТ]","payload_id":"demo1"}'`)}>Копировать</Button>
        </Cell>
        <Cell subtitle="fetch — маскирование">
          <pre style={{ fontSize: 11, whiteSpace: 'pre-wrap' }}>{`fetch('http://5.42.118.103:5173/process', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ payload: 'паспорт 4509 123456', payload_id: 'demo1' })
})`}</pre>
          <Button size="s" onClick={() => copy(`fetch('http://5.42.118.103:5173/process', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ payload: 'паспорт 4509 123456', payload_id: 'demo1' })
})`)}>Копировать</Button>
        </Cell>
        <Cell subtitle="fetch — демаскирование">
          <pre style={{ fontSize: 11, whiteSpace: 'pre-wrap' }}>{`fetch('http://5.42.118.103:5173/process', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ payload: 'паспорт [ПАСПОРТ]', payload_id: 'demo1' })
})`}</pre>
          <Button size="s" onClick={() => copy(`fetch('http://5.42.118.103:5173/process', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ payload: 'паспорт [ПАСПОРТ]', payload_id: 'demo1' })
})`)}>Копировать</Button>
        </Cell>
        <Cell subtitle="с access key (X-API-Key) — маскирование">
          <pre style={{ fontSize: 11, whiteSpace: 'pre-wrap' }}>{`curl -s -X POST http://5.42.118.103:5173/process \\
  -H 'Content-Type: application/json' \\
  -H 'X-API-Key: <access_key>' \\
  -d '{"payload":"паспорт 4509 123456","payload_id":"demo1","system":"chat"}'`}</pre>
          <Button size="s" onClick={() => copy(`curl -s -X POST http://5.42.118.103:5173/process \\
  -H 'Content-Type: application/json' \\
  -H 'X-API-Key: <access_key>' \\
  -d '{"payload":"паспорт 4509 123456","payload_id":"demo1","system":"chat"}'`)}>Копировать</Button>
        </Cell>
      </Section>

      <Section header="Логи">
        <Cell subtitle="скачать файл логов сервера (pii-gateway.log)">
          <Button size="s" onClick={downloadLogs}>Скачать логи</Button>
        </Cell>
      </Section>

      {(create.isError || remove.isError) && (
        <div style={{ color: 'red' }}>Ошибка: {String(create.error || remove.error)}</div>
      )}

      {toast && (
        <div style={{
          position: 'fixed',
          bottom: 24,
          left: '50%',
          transform: 'translateX(-50%)',
          background: 'rgba(0,0,0,0.85)',
          color: '#fff',
          padding: '10px 18px',
          borderRadius: 8,
          fontSize: 14,
          zIndex: 1000,
          boxShadow: '0 2px 8px rgba(0,0,0,0.3)',
        }}>
          {toast}
        </div>
      )}
    </Section>
  );
}