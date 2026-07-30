export interface LockedPosition {
  id: string;
  amount: number;
  currency: string;
  locked_at: string;
  matures_at: string;
  boost_percent: number;
  yield_earned: number;
}

export interface ProjectionBandPoint {
  date: string;
  median: number;
  upper_bound: number;
  lower_bound: number;
}

export interface ProjectionData {
  vault_id?: string;
  currency: string;
  current_apy: number;
  timeline: ProjectionBandPoint[];
  success_probability: number;
  on_track: boolean;
  monthly_gap?: number;
}

export interface AssetComposition {
  asset: string;
  value: number;
  percentage: number;
  color: string;
}

export interface GoalProgressData {
  current_amount: number;
  target_amount: number;
  currency: string;
  principal_amount: number;
  yield_amount: number;
  locked_positions: LockedPosition[];
  flexible_amount: number;
  projection?: ProjectionData;
  asset_composition?: AssetComposition[];
  deadline: string;
  status?: string;
}
