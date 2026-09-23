import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Input, List, Section } from '@telegram-apps/telegram-ui';
import { api, type ConfigView, type RuleInfo } from './api';
import AdminKeyInput from './AdminKeyInput';

const emptyRule: RuleInfo = { type: '', regex: '', priority: 0, context: '', capture: '', keyword: '', confidence: 0.99 };

export default function RulesTab() {
  const qc = useQueryClient();
  const { data: cfg } = useQuery<ConfigView>({ queryKey: ['config'], queryFn: api.getConfig });
  const [draft, setDraft] = useState<RuleInfo>(emptyRule);
  const saveRules = useMutation({
    mutationFn: (rules: RuleInfo[]) => api.putConfig({ rules }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['config'] }),
  });

  const addRule = () => {
    const rules = [...(cfg?.rules ?? []), draft];
    saveRules.mutate(rules);
    setDraft(emptyRule);
  };

  return (
    <Section header="Overlay-правила ПДН (добавляет типы без переписывания ядра)">
      <AdminKeyInput />
      <List>
        {(cfg?.rules ?? []).map((r) => (
          <Section key={r.type} header={`${r.type} · conf ${r.confidence}`}>
            <div>{r.regex}</div>
            {r.context && <div>контекст: {r.context}</div>}
          </Section>
        ))}
      </List>
      <Section header="Новое правило">
        <Input title="Тип" value={draft.type} onChange={(e) => setDraft({ ...draft, type: e.target.value })} placeholder="СНИЛС" />
        <Input title="Regex (RE2)" value={draft.regex} onChange={(e) => setDraft({ ...draft, regex: e.target.value })} />
        <Input title="Контекст (regex)" value={draft.context} onChange={(e) => setDraft({ ...draft, context: e.target.value })} />
        <Input title="Confidence" type="number" step="0.01" value={String(draft.confidence)} onChange={(e) => setDraft({ ...draft, confidence: Number(e.target.value) })} />
        <Button onClick={addRule}>Добавить правило</Button>
      </Section>
      {saveRules.isError && <div style={{ color: 'red' }}>{String(saveRules.error)}</div>}
    </Section>
  );
}
