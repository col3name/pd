import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Input, List, Section } from '@telegram-apps/telegram-ui';
import { api, type CombinationInfo, type ConfigView } from './api';
import AdminKeyInput from './AdminKeyInput';

export default function CombinationsTab() {
  const qc = useQueryClient();
  const { data: cfg } = useQuery<ConfigView>({ queryKey: ['config'], queryFn: api.getConfig });
  const [type, setType] = useState('ПИН');
  const [requires, setRequires] = useState('КАРТА');
  const [window, setWindow] = useState('80');
  const save = useMutation({
    mutationFn: (combinations: CombinationInfo[]) => api.putConfig({ combinations }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['config'] }),
  });

  const add = () => {
    const next = [...(cfg?.combinations ?? []), { type, requires: requires.split(',').map((s) => s.trim()).filter(Boolean), window: Number(window) }];
    save.mutate(next);
  };

  return (
    <Section header="Комбинации типов (маскировать только вместе)">
      <AdminKeyInput />
      <List>
        {(cfg?.combinations ?? []).map((c) => (
          <Section key={`${c.type}-${c.requires.join('+')}`} header={c.type}>
            <div>требует: {c.requires.join(', ')} · window {c.window}</div>
          </Section>
        ))}
      </List>
      <Section header="Новая комбинация">
        <Input title="Тип (маскируется)" value={type} onChange={(e) => setType(e.target.value)} placeholder="ПИН" />
        <Input title="Требуемые типы (через запятую)" value={requires} onChange={(e) => setRequires(e.target.value)} placeholder="КАРТА" />
        <Input title="Window (байт)" value={window} onChange={(e) => setWindow(e.target.value)} />
        <Button onClick={add}>Добавить комбинацию</Button>
      </Section>
      {save.isError && <div style={{ color: 'red' }}>{String(save.error)}</div>}
    </Section>
  );
}
