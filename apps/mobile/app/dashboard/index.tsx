import { View, Text, ScrollView, StyleSheet, TouchableOpacity } from "react-native";
import { useRouter } from "expo-router";

const metrics = [
  { label: "Total Balance", value: "$0.00", sub: "USDC" },
  { label: "Yield Earned", value: "$0.00", sub: "All time" },
  { label: "Active Vaults", value: "0", sub: "Positions" },
];

export default function DashboardScreen() {
  const router = useRouter();

  return (
    <ScrollView style={styles.container} contentContainerStyle={styles.content}>
      <Text style={styles.heading}>Portfolio Overview</Text>
      {metrics.map((m) => (
        <View key={m.label} style={styles.card}>
          <Text style={styles.cardLabel}>{m.label}</Text>
          <Text style={styles.cardValue}>{m.value}</Text>
          <Text style={styles.cardSub}>{m.sub}</Text>
        </View>
      ))}
      <TouchableOpacity style={styles.secondaryButton} onPress={() => router.push("/settings")}>
        <Text style={styles.secondaryButtonText}>Notification Settings</Text>
      </TouchableOpacity>
      <Text style={styles.hint}>Connect a wallet to see live data.</Text>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: "#0f172a" },
  content: { padding: 20 },
  heading: { fontSize: 22, fontWeight: "bold", color: "#ffffff", marginBottom: 20 },
  card: { backgroundColor: "#1e293b", borderRadius: 12, padding: 20, marginBottom: 12 },
  cardLabel: { color: "#94a3b8", fontSize: 13, marginBottom: 4 },
  cardValue: { color: "#ffffff", fontSize: 28, fontWeight: "bold", marginBottom: 2 },
  cardSub: { color: "#64748b", fontSize: 12 },
  secondaryButton: { borderColor: "#334155", borderWidth: 1, borderRadius: 10, paddingVertical: 14, alignItems: "center", marginTop: 8 },
  secondaryButtonText: { color: "#cbd5e1", fontSize: 15, fontWeight: "600" },
  hint: { color: "#475569", fontSize: 13, textAlign: "center", marginTop: 24 },
});
