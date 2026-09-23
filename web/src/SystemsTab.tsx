import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Cell, Input, List, Section, Switch } from '@telegram-apps/telegram-ui';
import { api, type SystemInfo } from './api';

export default function SystemsTab() {
  const qc = useQueryClient();
  const { data: systems, isLoading } = useQuery<SystemInfo[]>({ queryKey: ['systems'], queryFn: api.listSystems });
  const [name, setName] = useState('');
  const [newKey, setNewKey] = useState('');

  const invalidate = () => qc.invalidateQueries({ queryKey: ['systems'] });

  const create = useMutation({
    mutationFn: (n: string) => api.createSystem({ name: n }),
    onSuccess: (res) => { setNewKey(res.access_key); setName(''); invalidate(); },
  });
  const update = useMutation({
    mutationFn: ({ name, body }: { name: string; body: { enabled?: boolean; allow_unmask?: boolean } }) =>
      api.updateSystem(name, body),
    onSuccess: invalidate,
  });
  const remove = useMutation({ mutationFn: (n: string) => api.deleteSystem(n), onSuccess: invalidate });
  const regen = useMutation({
    mutationFn: (n: string) => api.regenerateKey(n),
    onSuccess: (res) => setNewKey(res.access_key),
  });

  if (isLoading) return <div>Загрузка…</div>;

  const copy = (v: string) => navigator.clipboard?.writeText(v);

  return (
    <Section header="Системы-потребители">
      <List>
        {(systems ?? []).map((s) => (
          <Section key={s.name} header={s.name}>
            <Cell
              subtitle={`allow_unmask: ${s.allow_unmask ? 'да' : 'нет'} · режим: ${s.masking || 'глобальный'}`}
              after={<Switch checked={s.enabled} onChange={(e) => update.mutate({ name: s.name, body: { enabled: e.target.checked } })} />}
            >
              {s.enabled ? 'Включена' : 'Отключена'}
            </Cell>
            <Cell
              subtitle="Демаскирование"
              after={<Switch checked={s.allow_unmask} onChange={(e) => update.mutate({ name: s.name, body: { allow_unmask: e.target.checked } })} />}
            >
              {s.allow_unmask ? 'Разрешено' : 'Запрещено'}
            </Cell>
            <Cell subtitle="Разрешённые типы ПДН">
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                {(s.pii ?? []).map((t) => <span key={t}>{t}</span>)}
              </div>
            </Cell>
            <Cell>
              <Button size="s" onClick={() => regen.mutate(s.name)}>Новый ключ</Button>
              <Button size="s" onClick={() => remove.mutate(s.name)}>Удалить</Button>
            </Cell>
          </Section>
        ))}
      </List>

      <Section header="Новая система">
        <Input title="Имя" value={name} onChange={(e) => setName(e.target.value)} placeholder="chat" />
        <Button onClick={() => create.mutate(name)} disabled={!name || create.isPending}>Создать</Button>
      </Section>

      {newKey && (
        <Section header="Новый ключ (показывается один раз)">
          <Cell subtitle="скопируйте и сохраните">
            <code>{newKey}</code>
          </Cell>
          <Button onClick={() => copy(newKey)}>Копировать</Button>
        </Section>
      )}

      {(create.isError || update.isError || remove.isError || regen.isError) && (
        <div style={{ color: 'red' }}>Ошибка: {String(create.error || update.error || remove.error || regen.error)}</div>
      )}
    </Section>
  );
}