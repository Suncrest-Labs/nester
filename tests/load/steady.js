import exec from 'k6/execution';
import { endpointTargets, readThresholds, requestFor, websocket } from './common.js';
const scenarios = Object.fromEntries(Object.entries(endpointTargets).map(([name, rate]) => [name, { executor: 'constant-arrival-rate', rate: rate / 2, timeUnit: '1s', duration: '10m', preAllocatedVUs: Math.max(10, Math.ceil(rate / 5)), maxVUs: Math.max(50, rate) }]));
scenarios.websocket = { executor: 'constant-vus', vus: 500, duration: '10m', gracefulStop: '10s' };
export const options = { scenarios, thresholds: readThresholds, tags: { test_type: 'steady' } };
export default function () { return exec.scenario.name === 'websocket' ? websocket() : requestFor(exec.scenario.name); }
