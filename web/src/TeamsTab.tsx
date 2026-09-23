import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Cell, Input, List, Section } from '@telegram-apps/telegram-ui';
import { api, type ConfigView, type SystemInfo } from './api';
import TeamCard from './TeamCard';

export default function TeamsTab() {
  const qc = useQueryClient();
  const { data: teams, isLoading } = useQuery<SystemInfo[]>({ queryKey: ['systems'], queryFn: api.listSystems });
  const { data: cfg } = useQuery<ConfigView>({ queryKey: ['config'], queryFn: api.getConfig });
  const [name, setName] = useState('');

  const invalidate = () => qc.invalidateQueries({ queryKey: ['systems'] });

  const create = useMutation({
    mutationFn: (n: string) => api.createSystem({ name: n }),
    onSuccess: () => { setName(''); invalidate(); },
  });
  const remove = useMutation({ mutationFn: (n: string) => api.deleteSystem(n), onSuccess: invalidate });

  if (isLoading) return <div>Загрузка…</div>;

  const knownTypes = cfg?.known_types ?? [];

  return (
    <Section header="Команды">
      <List>
        {(teams ?? []).map((t) => (
          <Section key={t.name} header={t.name}>
            <TeamCard team={t} knownTypes={knownTypes} onChanged={invalidate} />
            <Cell>
              <Button size="s" onClick={() => remove.mutate(t.name)}>Удалить команду</Button>
            </Cell>
          </Section>
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