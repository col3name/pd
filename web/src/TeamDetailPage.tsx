import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams } from 'react-router-dom';
import { Button, Cell, List, Section, Switch } from '@telegram-apps/telegram-ui';
import { api, type ConfigView, type SystemInfo } from './api';

export default function TeamDetailPage() {
  const { name = '' } = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { data: teams, isLoading } = useQuery<SystemInfo[]>({ queryKey: ['systems'], queryFn: api.listSystems });
  const { data: cfg } = useQuery<ConfigView>({ queryKey: ['config'], queryFn: api.getConfig });
  const [newKey, setNewKey] = useState('');

  const team = (teams ?? []).find((t) => t.name === name);
  const knownTypes = cfg?.known_types ?? [];

  const invalidate = () => qc.invalidateQueries({ queryKey: ['systems'] });

  const update = useMutation({
    mutationFn: (body: { enabled?: boolean; pii?: string[]; require_key?: boolean; masking?: string; allow_unmask?: boolean }) =>
      api.updateSystem(name, body),
    onSuccess: invalidate,
  });
  const regen = useMutation({
    mutationFn: () => api.regenerateKey(name),
    onSuccess: (res) => setNewKey(res.access_key),
  });
  const remove = useMutation({
    mutationFn: () => api.deleteSystem(name),
    onSuccess: () => navigate('/'),
  });

  if (isLoading) return <div>Загрузка…</div>;
  if (!team) return <div>Команда не найдена</div>;

  const copy = (v: string) => navigator.clipboard?.writeText(v);

  const enabledSet = new Set(team.pii ?? []);
  const allEnabled = (team.pii ?? []).length === 0;

  const toggleType = (type: string, on: boolean) => {
    if (on) {
      if (allEnabled) return;
      const next = [...(team.pii ?? []), type];
      const coversAll = knownTypes.every((t) => next.includes(t));
      update.mutate({ pii: coversAll ? [] : next });
    } else {
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
      <Cell>
        <Button size="s" onClick={() => navigate('/')}>← Назад</Button>
      </Cell>

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

      <Section header="Управление доступом">
        <Cell
          subtitle="требовать access key для /process"
          after={<Switch checked={team.require_key} onChange={(e) => update.mutate({ require_key: e.target.checked })} />}
        >
          Требовать access key
        </Cell>
        <Cell
          subtitle="разрешить демаскирование"
          after={<Switch checked={team.allow_unmask} onChange={(e) => update.mutate({ allow_unmask: e.target.checked })} />}
        >
          Разрешить демаскирование
        </Cell>
        <Cell subtitle="режим маскирования">
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            {['redact', 'token', 'synthetic'].map((m) => (
              <Button
                key={m}
                size="s"
                mode={team.masking === m ? 'filled' : 'bezeled'}
                onClick={() => update.mutate({ masking: m })}
              >
                {m}
              </Button>
            ))}
          </div>
        </Cell>
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

      <Cell>
        <Button size="s" onClick={() => remove.mutate()}>Удалить команду</Button>
      </Cell>

      {(update.isError || regen.isError || remove.isError) && (
        <div style={{ color: 'red' }}>Ошибка: {String(update.error || regen.error || remove.error)}</div>
      )}
    </Section>
  );
}