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

      {(create.isError || remove.isError) && (
        <div style={{ color: 'red' }}>Ошибка: {String(create.error || remove.error)}</div>
      )}
    </Section>
  );
}