import { useEffect, useState } from "react";
import { ActivityIndicator, ScrollView, StyleSheet, Switch, Text, View } from "react-native";
import {
  getNotificationPreferences,
  NotificationPreferences,
  updateNotificationPreferences,
} from "../../lib/notifications";

type PreferenceKey = keyof NotificationPreferences;

const rows: Array<{ key: PreferenceKey; label: string; detail: string }> = [
  { key: "push", label: "Push Notifications", detail: "Deposits, yield milestones, and settlements" },
  { key: "email", label: "Email", detail: "Account and portfolio updates" },
  { key: "websocket", label: "Live In-App Updates", detail: "Realtime dashboard events" },
];

export default function SettingsScreen() {
  const [preferences, setPreferences] = useState<NotificationPreferences>({
    email: true,
    websocket: true,
    push: true,
  });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState<PreferenceKey | null>(null);

  useEffect(() => {
    let alive = true;
    getNotificationPreferences()
      .then((next) => {
        if (alive) {
          setPreferences(next);
        }
      })
      .catch(() => undefined)
      .finally(() => {
        if (alive) {
          setLoading(false);
        }
      });
    return () => {
      alive = false;
    };
  }, []);

  const setPreference = async (key: PreferenceKey, value: boolean) => {
    const previous = preferences;
    const next = { ...preferences, [key]: value };
    setPreferences(next);
    setSaving(key);
    try {
      setPreferences(await updateNotificationPreferences({ [key]: value }));
    } catch {
      setPreferences(previous);
    } finally {
      setSaving(null);
    }
  };

  if (loading) {
    return (
      <View style={styles.centered}>
        <ActivityIndicator color="#38bdf8" />
      </View>
    );
  }

  return (
    <ScrollView style={styles.container} contentContainerStyle={styles.content}>
      <Text style={styles.heading}>Notifications</Text>
      {rows.map((row) => (
        <View key={row.key} style={styles.row}>
          <View style={styles.copy}>
            <Text style={styles.label}>{row.label}</Text>
            <Text style={styles.detail}>{row.detail}</Text>
          </View>
          <Switch
            value={preferences[row.key]}
            onValueChange={(value) => setPreference(row.key, value)}
            disabled={saving !== null}
            trackColor={{ false: "#334155", true: "#2563eb" }}
            thumbColor={preferences[row.key] ? "#eff6ff" : "#94a3b8"}
          />
        </View>
      ))}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: "#0f172a" },
  content: { padding: 20 },
  centered: { flex: 1, alignItems: "center", justifyContent: "center", backgroundColor: "#0f172a" },
  heading: { fontSize: 22, fontWeight: "bold", color: "#ffffff", marginBottom: 20 },
  row: { backgroundColor: "#1e293b", borderRadius: 12, padding: 18, marginBottom: 12, flexDirection: "row", alignItems: "center" },
  copy: { flex: 1, paddingRight: 16 },
  label: { color: "#ffffff", fontSize: 16, fontWeight: "600", marginBottom: 4 },
  detail: { color: "#94a3b8", fontSize: 13, lineHeight: 18 },
});
