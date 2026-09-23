import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Section } from '@telegram-apps/telegram-ui';
import { api, getAdminKey, setAdminKey, type ConfigView } from './api';
import AdminKeyInput from './AdminKeyInput';

export default function ConfigTab() {
  const qc = useQueryClient();
  const { data: cfg } = useQuery<ConfigView>({ queryKey: ['config'], queryFn: api.getConfig });
  const [text, setText] = useState('');
  const [key, setKey] = useState(getAdminKey());
  const apply = useMutation({
    mutationFn: (body: string) => api.putConfig(JSON.parse(body)),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['config'] }),
  });

  useEffect(() => {
    if (cfg) {
      const { rev: _rev, known_types: _kt, ...rest } = cfg;
      setText(JSON.stringify({ ...rest, systems: (rest.systems ?? []).map((s) => ({ ...s, api_key: null })) }, null, 2));
    }
  }, [cfg]);

  return (
    <Section header="Конфиг (JSON)">
      <AdminKeyInput />
      <textarea
        style={{ width: '100%', minHeight: 400, fontFamily: 'monospace', fontSize: 12 }}
        value={text}
        onChange={(e) => setText(e.target.value)}
      />
      <Button mode="filled" onClick={() => { if (key) setAdminKey(key); apply.mutate(text); }} disabled={apply.isPending}>
        Применить (PUT /v1/config)
      </Button>
      {apply.isSuccess && <div style={{ color: 'green' }}>Применено, rev={apply.data.rev}</div>}
      {apply.isError && <div style={{ color: 'red' }}>{String(apply.error)}</div>}
    </Section>
  );
}
