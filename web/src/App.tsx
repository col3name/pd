import { useState } from 'react';
import { Cell, List, Section } from '@telegram-apps/telegram-ui';
import SystemsTab from './SystemsTab';
import RulesTab from './RulesTab';
import CombinationsTab from './CombinationsTab';
import ConfigTab from './ConfigTab';

type Tab = 'systems' | 'rules' | 'combinations' | 'config';

const TABS: Array<{ id: Tab; label: string }> = [
  { id: 'systems', label: 'Системы' },
  { id: 'rules', label: 'Правила ПДН' },
  { id: 'combinations', label: 'Комбинации' },
  { id: 'config', label: 'Конфиг' },
];

export default function App() {
  const [tab, setTab] = useState<Tab>('systems');
  return (
    <div style={{ maxWidth: 720, margin: '0 auto', padding: 16 }}>
      <Section header="PII Gateway — Админка">
        <List>
          {TABS.map((t) => (
            <Cell key={t.id} subtitle={tab === t.id ? 'активен' : undefined} onClick={() => setTab(t.id)}>
              {t.label}
            </Cell>
          ))}
        </List>
      </Section>
      {tab === 'systems' && <SystemsTab />}
      {tab === 'rules' && <RulesTab />}
      {tab === 'combinations' && <CombinationsTab />}
      {tab === 'config' && <ConfigTab />}
    </div>
  );
}