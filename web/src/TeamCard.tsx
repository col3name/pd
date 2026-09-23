import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Button, Cell, List, Section, Switch } from '@telegram-apps/telegram-ui';
import { api, type SystemInfo } from './api';

interface Props {
  team: SystemInfo;
  knownTypes: string[];
  onChanged: () => void;
}

export default function TeamCard({ team, knownTypes, onChanged }: Props) {
  const qc = useQueryClient();
  const [newKey, setNewKey] = useState('');

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['systems'] });
    onChanged();
  };

  const update = useMutation({
    mutationFn: (body: { enabled?: boolean; pii?: string[] }) => api.updateSystem(team.name, body),
    onSuccess: invalidate,
  });
  const regen = useMutation({
    mutationFn: () => api.regenerateKey(team.name),
    onSuccess: (res) => setNewKey(res.access_key),
  });

  const copy = (v: string) => navigator.clipboard?.writeText(v);

  // pii empty = all types enabled.
  const enabledSet = new Set(team.pii ?? []);
  const allEnabled = (team.pii ?? []).length === 0;

  const toggleType = (type: string, on: boolean) => {
    if (on) {
      // Turning ON: if all enabled already, nothing to do.
      if (allEnabled) return;
      const next = [...(team.pii ?? []), type];
      // If now covers all known types, send empty (all).
      const coversAll = knownTypes.every((t) => next.includes(t));
      update.mutate({ pii: coversAll ? [] : next });
    } else {
      // Turning OFF: if all enabled, start from all known types minus this one.
      const base = allEnabled ? knownTypes : (team.pii ?? []);
      const next = base.filter((t) => t !== type);
      update.mutate({ pii: next });
    }
  };

  const accessKey = newKey || (team.api_key_set ? '••••••••••••••••••••••••••••••••' : '');

  const curlMask = `curl -s -X POST http://localhost:8080/process \\
  -H "X-API-Key: ${accessKey}" \\
  -H 'Content-Type: application/json' \\
  -d '{"payload":"паспорт 4509 123456","payload_id":"demo1","system":"${team.name}"}'`;

  const fetchMask = `fetch('http://localhost:8080/process', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json', 'X-API-Key': '${accessKey}' },
  body: JSON.stringify({ payload: 'паспорт 4509 123456', payload_id: 'demo1', system: '${team.name}' })
})`;

  return (
    <Section header={team.name}>
      <Cell
        subtitle={team.enabled ? 'Доступ включён' : 'Доступ отключён'}
        after={<Switch checked={team.enabled} onChange={(e) => update.mutate({ enabled: e.target.checked })} />}
      >
        {team.enabled ? 'Включена' : 'Отключена'}
      </Cell>

      <Section header="Обнаружение типов ПД">
        <List>
          {knownTypes.map((t) => (
            <Cell
              key={t}
              subtitle={allEnabled || enabledSet.has(t) ? 'маскируется' : 'не маскируется'}
              after={<Switch checked={allEnabled || enabledSet.has(t)} onChange={(e) => toggleType(t, e.target.checked)} />}
            >
              {t}
            </Cell>
          ))}
        </List>
      </Section>

      <Section header="AccessKey">
        <Cell subtitle="ключ для вызовов /process">
          <code>{accessKey || 'Ключ не задан'}</code>
        </Cell>
        <Cell>
          <Button size="s" onClick={() => regen.mutate()}>Новый ключ</Button>
          {accessKey && <Button size="s" onClick={() => copy(accessKey)}>Копировать</Button>}
        </Cell>
      </Section>

      {accessKey && (
        <Section header="Примеры использования">
          <Cell subtitle="curl — маскирование">
            <pre style={{ fontSize: 11, whiteSpace: 'pre-wrap' }}>{curlMask}</pre>
          </Cell>
          <Cell subtitle="fetch — маскирование">
            <pre style={{ fontSize: 11, whiteSpace: 'pre-wrap' }}>{fetchMask}</pre>
          </Cell>
          <Cell subtitle="демаскирование — тот же запрос, но payload = результат маскирования, тот же payload_id">
            <pre style={{ fontSize: 11, whiteSpace: 'pre-wrap' }}>{`// повторный запрос с маской возвращает оригинал`}</pre>
          </Cell>
        </Section>
      )}

      {update.isError && <div style={{ color: 'red' }}>Ошибка: {String(update.error)}</div>}
      {regen.isError && <div style={{ color: 'red' }}>Ошибка: {String(regen.error)}</div>}
    </Section>
  );
}