import exec from 'k6/execution';
import { endpointTargets, readThresholds, requestFor, websocket } from './common.js';
const scenarios = Object.fromEntries(Object.entries(endpointTargets).map(([name, rate]) => [name, { executor: 'ramping-arrival-rate', startRate: 0, timeUnit: '1s', preAllocatedVUs: Math.max(20, Math.ceil(rate / 2)), maxVUs: Math.max(100, rate * 3), stages: [{ target: rate * 2, duration: '5m' }] }]));
scenarios.websocket = { executor: 'ramping-vus', startVUs: 0, stages: [{ target: 2000, duration: '5m' }], gracefulRampDown: '10s' };
export const options = { scenarios, thresholds: readThresholds, tags: { test_type: 'ramp' } };
export default function () { return exec.scenario.name === 'websocket' ? websocket() : requestFor(exec.scenario.name); }
