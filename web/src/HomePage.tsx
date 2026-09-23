import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { Button, Cell, Input, List, Section } from '@telegram-apps/telegram-ui';
import { api, type SystemInfo } from './api';

export default function HomePage() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { data: teams, isLoading } = useQuery<SystemInfo[]>({ queryKey: ['systems'], queryFn: api.listSystems });
  const [name, setName] = useState('');

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
          <pre style={{ fontSize: 11, whiteSpace: 'pre-wrap' }}>{`curl -s -X POST /process \\
  -H 'Content-Type: application/json' \\
  -d '{"payload":"паспорт 4509 123456","payload_id":"demo1"}'`}</pre>
        </Cell>
        <Cell subtitle="curl — демаскирование (тот же payload_id, payload = маска)">
          <pre style={{ fontSize: 11, whiteSpace: 'pre-wrap' }}>{`curl -s -X POST /process \\
  -H 'Content-Type: application/json' \\
  -d '{"payload":"паспорт [ПАСПОРТ]","payload_id":"demo1"}'`}</pre>
        </Cell>
        <Cell subtitle="fetch — маскирование">
          <pre style={{ fontSize: 11, whiteSpace: 'pre-wrap' }}>{`fetch('/process', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ payload: 'паспорт 4509 123456', payload_id: 'demo1' })
})`}</pre>
        </Cell>
        <Cell subtitle="fetch — демаскирование">
          <pre style={{ fontSize: 11, whiteSpace: 'pre-wrap' }}>{`fetch('/process', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ payload: 'паспорт [ПАСПОРТ]', payload_id: 'demo1' })
})`}</pre>
        </Cell>
        <Cell subtitle="с access key (X-API-Key) — маскирование">
          <pre style={{ fontSize: 11, whiteSpace: 'pre-wrap' }}>{`curl -s -X POST /process \\
  -H 'Content-Type: application/json' \\
  -H 'X-API-Key: <access_key>' \\
  -d '{"payload":"паспорт 4509 123456","payload_id":"demo1","system":"chat"}'`}</pre>
        </Cell>
      </Section>

      {(create.isError || remove.isError) && (
        <div style={{ color: 'red' }}>Ошибка: {String(create.error || remove.error)}</div>
      )}
    </Section>
  );
}