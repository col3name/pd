import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Cell, Checkbox, Input, List, Section, Switch } from '@telegram-apps/telegram-ui';
import { api, type ConfigView, type SystemInfo } from './api';
import AdminKeyInput from './AdminKeyInput';

export default function SystemsTab() {
  const qc = useQueryClient();
  const { data: cfg, isLoading } = useQuery<ConfigView>({ queryKey: ['config'], queryFn: api.getConfig });
  const save = useMutation({
    mutationFn: (systems: SystemInfo[]) => api.putConfig({ systems }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['config'] }),
  });

  if (isLoading) return <div>Загрузка…</div>;
  if (!cfg) return <div>Нет данных</div>;

  const update = (i: number, patch: Partial<SystemInfo>) => {
    const next = [...(cfg.systems ?? [])];
    next[i] = { ...next[i], ...patch };
    save.mutate(next);
  };

  return (
    <Section header="Системы-потребители">
      <AdminKeyInput />
      <List>
        {(cfg.systems ?? []).map((s, i) => (
          <Section key={s.name} header={s.name}>
            <Cell
              subtitle={`allow_unmask: ${s.allow_unmask ? 'да' : 'нет'} · режим: ${s.masking}`}
              after={<Switch checked={s.enabled} onChange={(e) => update(i, { enabled: e.target.checked })} />}
            >
              {s.enabled ? 'Включена' : 'Отключена'}
            </Cell>
            <Cell subtitle="Разрешённые типы ПДН">
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                {(s.pii ?? []).map((t) => (
                  <span key={t}>{t}</span>
                ))}
              </div>
            </Cell>
          </Section>
        ))}
      </List>
      {save.isError && <div style={{ color: 'red' }}>Ошибка: {String(save.error)}</div>}
      {save.isSuccess && <div style={{ color: 'green' }}>Сохранено (rev {save.data.rev})</div>}
    </Section>
  );
}
