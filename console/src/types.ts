export type Me = { user: string; expiresAt: string; signingKeyId?: string };
export type Revision = { seq: number; version: string; ruleSet: Record<string, unknown>; createdAt: string; createdBy?: string };
export type Machine = {
  id: string; user: string; name?: string; os?: string; enrolledAt: string; lastSeenAt: string;
  lastBundleVersion?: string; lastKeyId?: string;
};
export type ScimCounts = { users: number; activeUsers: number; groups: number };
export type GroupsView = {
  hasSnapshot: boolean; source?: string; appliedBy?: string; syncedAt?: string; users?: number; groups?: number;
  members?: Record<string, string[]>; scim?: ScimCounts;
};
export type GroupSources = {
  user: string; authored: string[]; snapshot: string[]; scim: string[]; effective: string[]; scimNearMatch: string | null;
};
export type AuditEvent = { id: number; at: string; actor: string; action: string; target?: string; detail?: Record<string, unknown> };
export type EnrollmentToken = { token: string; user: string; expiresAt: string };
