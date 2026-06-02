import { Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { useEffect } from "react";
import { registerCurrentDeviceToken } from "../lib/notifications";

export default function RootLayout() {
  useEffect(() => {
    registerCurrentDeviceToken().catch(() => undefined);
  }, []);

  return (
    <>
      <StatusBar style="light" />
      <Stack
        screenOptions={{
          headerStyle: { backgroundColor: "#0f172a" },
          headerTintColor: "#ffffff",
          headerTitleStyle: { fontWeight: "bold" },
          contentStyle: { backgroundColor: "#0f172a" },
        }}
      >
        <Stack.Screen name="index" options={{ title: "Nester" }} />
        <Stack.Screen name="dashboard/index" options={{ title: "Dashboard" }} />
        <Stack.Screen name="settings/index" options={{ title: "Settings" }} />
        <Stack.Screen name="vaults/index" options={{ title: "Vaults" }} />
      </Stack>
    </>
  );
}
