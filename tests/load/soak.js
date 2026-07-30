import exec from 'k6/execution';
import { endpointTargets, readThresholds, requestFor, websocket } from './common.js';
const scenarios = Object.fromEntries(Object.entries(endpointTargets).map(([name, rate]) => [name, { executor: 'constant-arrival-rate', rate: rate / 5, timeUnit: '1s', duration: '2h', preAllocatedVUs: Math.max(10, Math.ceil(rate / 10)), maxVUs: Math.max(50, rate) }]));
scenarios.websocket = { executor: 'constant-vus', vus: 200, duration: '2h', gracefulStop: '10s' };
export const options = { scenarios, thresholds: readThresholds, tags: { test_type: 'soak' } };
export default function () { return exec.scenario.name === 'websocket' ? websocket() : requestFor(exec.scenario.name); }
