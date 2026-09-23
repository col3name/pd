import { Input } from '@telegram-apps/telegram-ui';
import { getAdminKey, setAdminKey } from './api';

export default function AdminKeyInput() {
  return (
    <Input
      title="Admin-ключ"
      placeholder="X-Admin-Key"
      defaultValue={getAdminKey()}
      onChange={(e) => setAdminKey(e.target.value)}
    />
  );
}
