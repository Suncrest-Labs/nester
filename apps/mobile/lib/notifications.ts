import * as Notifications from "expo-notifications";
import { Platform } from "react-native";
import { apiRequest } from "./api";

export type NotificationPreferences = {
  email: boolean;
  websocket: boolean;
  push: boolean;
};

type DeviceTokenResponse = {
  token: string;
  platform: string;
};

export async function requestDevicePushToken(): Promise<string | null> {
  if (Platform.OS === "web") {
    return null;
  }

  const existing = await Notifications.getPermissionsAsync();
  const finalStatus =
    existing.status === "granted"
      ? existing.status
      : (await Notifications.requestPermissionsAsync()).status;

  if (finalStatus !== "granted") {
    return null;
  }

  const token = await Notifications.getDevicePushTokenAsync();
  return String(token.data);
}

export async function registerCurrentDeviceToken(): Promise<DeviceTokenResponse | null> {
  const token = await requestDevicePushToken();
  if (!token) {
    return null;
  }

  return apiRequest<DeviceTokenResponse>("/api/v1/users/device-tokens", {
    method: "POST",
    body: {
      token,
      platform: Platform.OS,
    },
  });
}

export function getNotificationPreferences(): Promise<NotificationPreferences> {
  return apiRequest<NotificationPreferences>("/api/v1/users/notification-preferences");
}

export function updateNotificationPreferences(
  patch: Partial<NotificationPreferences>,
): Promise<NotificationPreferences> {
  return apiRequest<NotificationPreferences>("/api/v1/users/notification-preferences", {
    method: "PATCH",
    body: patch,
  });
}
