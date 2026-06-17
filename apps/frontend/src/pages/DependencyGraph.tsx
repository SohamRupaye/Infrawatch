import { useCallback, useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import {
  ReactFlow,
  useNodesState,
  useEdgesState,
  useReactFlow,
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  Handle,
  Position,
  getBezierPath,
  MarkerType,
  type NodeProps,
  type EdgeProps,
  type Node,
  type Edge,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { fetchServices } from '@/api/services';
import { fetchConfigServices } from '@/api/config';
import { useLiveStore } from '@/store/liveStore';
import { Spinner } from '@/components/Spinner';
import type { ServiceState } from '@/api/types';

// ─── Constants ────────────────────────────────────────────────────────────────

const NODE_W = 192;
const H_GAP = 56;
const V_GAP = 80;

// Latency bar turns amber at or above this threshold
const LATENCY_WARN_MS = 100;

const STATE_COLOR: Record<ServiceState, string> = {
  HEALTHY: '#10b981',
  DEGRADED: '#f59e0b',
  UNHEALTHY: '#f97316',
  DEAD: '#ef4444',
  RECOVERING: '#6366f1',
  UNKNOWN: '#475569',
};

const STATE_LABEL: Record<ServiceState, string> = {
  HEALTHY: 'Healthy',
  DEGRADED: 'Degraded',
  UNHEALTHY: 'Unhealthy',
  DEAD: 'Dead',
  RECOVERING: 'Recovering',
  UNKNOWN: 'Unknown',
};

// Max latency used to scale the latency bar (anything ≥ this fills 100%)
const LATENCY_BAR_MAX_MS = 500;

const IGW_ID = '__igw__';
const IGW_W = 168;
const IGW_H = 60;

// ─── Node data types ─────────────────────────────────────────────────────────

interface ServiceNodeData extends Record<string, unknown> {
  name: string;
  state: ServiceState;
  uptime: number;
  responseMs: number;
}

// ─── Floating-edge geometry helpers ─────────────────────────────────────────

// Fixed heights used only for edge routing geometry (nodes are auto-height in DOM)
const NODE_H = 68;
const IGW_H_ROUTING = IGW_H;

function nodeSize(n: Node): { w: number; h: number } {
  if (n.id === IGW_ID) return { w: IGW_W, h: IGW_H_ROUTING };
  return { w: NODE_W, h: NODE_H };
}

function getFloatingEdgeParams(
  sourceNode: Node,
  targetNode: Node,
): {
  sx: number;
  sy: number;
  tx: number;
  ty: number;
  sourcePos: Position;
  targetPos: Position;
} {
  const { w: sw, h: sh } = nodeSize(sourceNode);
  const { w: tw, h: th } = nodeSize(targetNode);
  const scx = sourceNode.position.x + sw / 2;
  const scy = sourceNode.position.y + sh / 2;
  const tcx = targetNode.position.x + tw / 2;
  const tcy = targetNode.position.y + th / 2;

  const dx = tcx - scx;
  const dy = tcy - scy;

  let sourcePos: Position, targetPos: Position;
  let sx: number, sy: number, tx: number, ty: number;

  if (Math.abs(dx) >= Math.abs(dy)) {
    if (dx > 0) {
      sourcePos = Position.Right;
      sx = sourceNode.position.x + sw;
      sy = scy;
      targetPos = Position.Left;
      tx = targetNode.position.x;
      ty = tcy;
    } else {
      sourcePos = Position.Left;
      sx = sourceNode.position.x;
      sy = scy;
      targetPos = Position.Right;
      tx = targetNode.position.x + tw;
      ty = tcy;
    }
  } else {
    if (dy > 0) {
      sourcePos = Position.Bottom;
      sx = scx;
      sy = sourceNode.position.y + sh;
      targetPos = Position.Top;
      tx = tcx;
      ty = targetNode.position.y;
    } else {
      sourcePos = Position.Top;
      sx = scx;
      sy = sourceNode.position.y;
      targetPos = Position.Bottom;
      tx = tcx;
      ty = targetNode.position.y + th;
    }
  }

  return { sx, sy, tx, ty, sourcePos, targetPos };
}

// ─── Floating edge component ──────────────────────────────────────────────────

interface FloatingEdgeData extends Record<string, unknown> {
  color: string;
  dashed: boolean;
  strokeWidth: number;
}

function FloatingEdge({
  id,
  source,
  target,
  data,
  markerEnd,
  style,
}: EdgeProps) {
  const { getNode } = useReactFlow();
  const sourceNode = getNode(source);
  const targetNode = getNode(target);

  if (!sourceNode || !targetNode) return null;

  const d = data as FloatingEdgeData;
  const { sx, sy, tx, ty, sourcePos, targetPos } = getFloatingEdgeParams(
    sourceNode,
    targetNode,
  );
  const [edgePath] = getBezierPath({
    sourceX: sx,
    sourceY: sy,
    sourcePosition: sourcePos,
    targetX: tx,
    targetY: ty,
    targetPosition: targetPos,
  });

  return (
    <path
      id={id}
      d={edgePath}
      fill="none"
      stroke={d.color}
      strokeWidth={d.strokeWidth ?? 1.5}
      strokeOpacity={d.dashed ? 0.7 : 0.5}
      strokeDasharray={d.dashed ? '5 4' : undefined}
      strokeLinecap="round"
      markerEnd={markerEnd as string | undefined}
      style={style as React.CSSProperties | undefined}
    />
  );
}

const EDGE_TYPES = { floating: FloatingEdge };

// ─── Service node ─────────────────────────────────────────────────────────────

// Invisible handles — needed for ReactFlow edge routing but not rendered visibly
const HANDLE_STYLE: React.CSSProperties = {
  width: 1,
  height: 1,
  background: 'transparent',
  border: 'none',
  opacity: 0,
  pointerEvents: 'none',
};

function Handles() {
  return (
    <>
      <Handle
        type="source"
        id="t"
        position={Position.Top}
        style={{ ...HANDLE_STYLE, top: 0 }}
      />
      <Handle
        type="source"
        id="b"
        position={Position.Bottom}
        style={{ ...HANDLE_STYLE, bottom: 0 }}
      />
      <Handle
        type="source"
        id="l"
        position={Position.Left}
        style={{ ...HANDLE_STYLE, left: 0 }}
      />
      <Handle
        type="source"
        id="r"
        position={Position.Right}
        style={{ ...HANDLE_STYLE, right: 0 }}
      />
      <Handle
        type="target"
        id="t"
        position={Position.Top}
        style={{ ...HANDLE_STYLE, top: 0 }}
      />
      <Handle
        type="target"
        id="b"
        position={Position.Bottom}
        style={{ ...HANDLE_STYLE, bottom: 0 }}
      />
      <Handle
        type="target"
        id="l"
        position={Position.Left}
        style={{ ...HANDLE_STYLE, left: 0 }}
      />
      <Handle
        type="target"
        id="r"
        position={Position.Right}
        style={{ ...HANDLE_STYLE, right: 0 }}
      />
    </>
  );
}

function ServiceNode({ data, selected }: NodeProps) {
  const d = data as ServiceNodeData;
  const color = STATE_COLOR[d.state] ?? STATE_COLOR.UNKNOWN;
  const slow = d.responseMs >= LATENCY_WARN_MS;

  return (
    <div
      style={{
        width: NODE_W,
        background: '#ffffff',
        border: selected ? `1.5px solid ${color}` : '1px solid #e2e5ef',
        borderRadius: 10,
        outline: selected ? `3px solid ${color}18` : 'none',
        outlineOffset: 2,
        display: 'flex',
        flexDirection: 'column',
        cursor: 'pointer',
        transition: 'border-color 0.15s, outline 0.15s',
        position: 'relative',
        boxSizing: 'border-box',
        overflow: 'hidden',
      }}
    >
      <Handles />

      {/* Main content */}
      <div style={{ padding: '10px 12px 8px' }}>
        {/* Name + dot inline */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 6,
            marginBottom: 5,
          }}
        >
          <div
            title={STATE_LABEL[d.state] ?? d.state}
            style={{
              width: 6,
              height: 6,
              borderRadius: '50%',
              background: color,
              flexShrink: 0,
            }}
          />
          <p
            style={{
              margin: 0,
              fontSize: 12,
              fontWeight: 600,
              color: '#1a1f35',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              flex: 1,
              letterSpacing: '-0.01em',
            }}
          >
            {d.name}
          </p>
        </div>
        {/* Uptime + latency value on same row */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
          }}
        >
          <span style={{ fontSize: 10, color: '#a0a6bf' }}>
            {d.uptime.toFixed(1)}% up
          </span>
          {d.responseMs > 0 && (
            <span
              style={{
                fontSize: 10,
                color: slow ? '#f59e0b' : '#a0a6bf',
                fontWeight: slow ? 600 : 400,
              }}
            >
              {d.responseMs}ms
            </span>
          )}
        </div>
      </div>

      {/* Latency bar — flush to bottom, acts as a bottom accent line */}
      {d.responseMs > 0 && (
        <div style={{ height: 3, background: '#f0f1f7', width: '100%' }}>
          <div
            style={{
              height: '100%',
              width: `${Math.min(100, Math.round((d.responseMs / LATENCY_BAR_MAX_MS) * 100))}%`,
              background: slow ? '#f59e0b' : '#10b981',
              transition: 'width 0.4s ease',
            }}
          />
        </div>
      )}
    </div>
  );
}

// ─── IGW node ────────────────────────────────────────────────────────────────

function IgwNode({ selected }: NodeProps) {
  return (
    <div
      style={{
        width: IGW_W,
        height: IGW_H,
        background: '#ffffff',
        border: `1px solid ${selected ? '#c8ccdb' : '#e2e5ef'}`,
        borderRadius: 10,
        outline: selected ? '3px solid #47556918' : 'none',
        outlineOffset: 2,
        display: 'flex',
        alignItems: 'center',
        gap: 10,
        padding: '0 14px',
        cursor: 'default',
        position: 'relative',
        boxSizing: 'border-box',
      }}
    >
      <Handles />
      {/* Minimal globe — muted, doesn't compete */}
      <svg
        width="18"
        height="18"
        viewBox="0 0 18 18"
        fill="none"
        style={{ flexShrink: 0, opacity: 0.45 }}
      >
        <circle cx="9" cy="9" r="7.5" stroke="#475569" strokeWidth="1" />
        <ellipse
          cx="9"
          cy="9"
          rx="3.2"
          ry="7.5"
          stroke="#475569"
          strokeWidth="1"
        />
        <line
          x1="1.5"
          y1="9"
          x2="16.5"
          y2="9"
          stroke="#475569"
          strokeWidth="1"
        />
        <line
          x1="3"
          y1="5.5"
          x2="15"
          y2="5.5"
          stroke="#475569"
          strokeWidth="0.75"
          strokeDasharray="1.5 1"
        />
        <line
          x1="3"
          y1="12.5"
          x2="15"
          y2="12.5"
          stroke="#475569"
          strokeWidth="0.75"
          strokeDasharray="1.5 1"
        />
      </svg>
      <div>
        <p
          style={{
            margin: 0,
            fontSize: 11,
            fontWeight: 600,
            color: '#1a1f35',
            letterSpacing: '-0.01em',
          }}
        >
          Internet
        </p>
        <p style={{ margin: 0, fontSize: 10, color: '#a0a6bf', marginTop: 1 }}>
          gateway
        </p>
      </div>
    </div>
  );
}

const NODE_TYPES = { service: ServiceNode, igw: IgwNode };

// ─── Layout algorithm (topological layered) ───────────────────────────────────

function buildLayout(
  cfgServices: Array<{ name: string; dependencies?: string[] }>,
  stateMap: Record<string, ServiceState>,
  uptimeMap: Record<string, number>,
  responseMsMap: Record<string, number>,
): { nodes: Node[]; edges: Edge[] } {
  if (cfgServices.length === 0) return { nodes: [], edges: [] };

  const depMap: Record<string, string[]> = {};
  for (const svc of cfgServices) {
    depMap[svc.name] = (svc.dependencies ?? []).filter((d) =>
      cfgServices.some((s) => s.name === d),
    );
  }

  const layerCache: Record<string, number> = {};
  const inStack = new Set<string>();

  function getLayer(name: string): number {
    if (name in layerCache) return layerCache[name];
    if (inStack.has(name)) {
      layerCache[name] = 0;
      return 0;
    }
    inStack.add(name);
    const deps = depMap[name] ?? [];
    const layer =
      deps.length === 0 ? 0 : Math.max(...deps.map((d) => getLayer(d))) + 1;
    layerCache[name] = layer;
    inStack.delete(name);
    return layer;
  }

  for (const svc of cfgServices) getLayer(svc.name);

  const maxLayer = Math.max(...Object.values(layerCache));
  const byLayer: string[][] = Array.from({ length: maxLayer + 1 }, () => []);
  for (const svc of cfgServices) byLayer[layerCache[svc.name]].push(svc.name);

  const nodes: Node[] = [];

  for (let layer = maxLayer; layer >= 0; layer--) {
    const row = byLayer[layer];
    const y = (maxLayer - layer + 1) * (NODE_H + V_GAP);
    const totalW = row.length * NODE_W + (row.length - 1) * H_GAP;
    const startX = -totalW / 2;

    row.forEach((name, col) => {
      nodes.push({
        id: name,
        type: 'service',
        position: { x: startX + col * (NODE_W + H_GAP), y },
        data: {
          name,
          state: stateMap[name] ?? 'UNKNOWN',
          uptime: uptimeMap[name] ?? 0,
          responseMs: responseMsMap[name] ?? 0,
        },
      });
    });
  }

  // IGW at y=0 above the top-most service layer
  nodes.push({
    id: IGW_ID,
    type: 'igw',
    position: { x: -IGW_W / 2, y: 0 },
    data: {},
    selectable: false,
    draggable: true,
  });

  const edges: Edge[] = cfgServices.flatMap((svc) =>
    (depMap[svc.name] ?? []).map((dep) => {
      const depState = stateMap[dep] ?? 'UNKNOWN';
      const isCritical = depState === 'DEAD' || depState === 'UNHEALTHY';
      const isHealthy = depState === 'HEALTHY';
      // Healthy → neutral gray solid; degraded → amber dashed; critical → red dashed thicker
      const color = isHealthy ? '#c8ccdb' : STATE_COLOR[depState];
      const dashed = !isHealthy;
      const strokeWidth = isCritical ? 2 : 1.5;
      return {
        id: `${svc.name}->${dep}`,
        source: svc.name,
        target: dep,
        type: 'floating',
        data: { color, dashed, strokeWidth } as Record<string, unknown>,
        markerEnd: {
          type: MarkerType.ArrowClosed,
          color,
          width: 12,
          height: 12,
        },
      };
    }),
  );

  const topServices = Object.entries(layerCache)
    .filter(([, l]) => l === maxLayer)
    .map(([name]) => name);

  const igwColor = '#c8ccdb';
  for (const svc of topServices) {
    edges.push({
      id: `${IGW_ID}->${svc}`,
      source: IGW_ID,
      target: svc,
      type: 'floating',
      data: { color: igwColor, dashed: false, strokeWidth: 1.5 } as Record<
        string,
        unknown
      >,
      markerEnd: {
        type: MarkerType.ArrowClosed,
        color: igwColor,
        width: 12,
        height: 12,
      },
    });
  }

  return { nodes, edges };
}

// ─── Page component ───────────────────────────────────────────────────────────

export function DependencyGraph() {
  const navigate = useNavigate();
  const liveStates = useLiveStore((s) => s.serviceStates);
  const [selected, setSelected] = useState<string | null>(null);

  const { data: svcsData, isLoading: svcsLoading } = useQuery({
    queryKey: ['services'],
    queryFn: () => fetchServices(),
    refetchInterval: 30_000,
  });

  const { data: cfgData, isLoading: cfgLoading } = useQuery({
    queryKey: ['config-services'],
    queryFn: () => fetchConfigServices(),
    refetchInterval: 60_000,
  });

  const stateMap = useMemo(() => {
    const m: Record<string, ServiceState> = {};
    for (const svc of svcsData?.services ?? [])
      m[svc.name] = liveStates[svc.name]?.state ?? svc.state;
    return m;
  }, [svcsData, liveStates]);

  const uptimeMap = useMemo(() => {
    const m: Record<string, number> = {};
    for (const svc of svcsData?.services ?? []) m[svc.name] = svc.uptime_pct;
    return m;
  }, [svcsData]);

  const responseMsMap = useMemo(() => {
    const m: Record<string, number> = {};
    for (const svc of svcsData?.services ?? [])
      m[svc.name] = svc.response_time_ms;
    return m;
  }, [svcsData]);

  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);

  // Rebuild layout only when config changes (preserves drag positions on live updates)
  useEffect(() => {
    if (!cfgData) return;
    const { nodes: n, edges: e } = buildLayout(
      cfgData.services,
      stateMap,
      uptimeMap,
      responseMsMap,
    );
    setNodes(n);
    setEdges(e);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cfgData]);

  // Patch node data & edge colors on live state changes without resetting positions
  useEffect(() => {
    setNodes((nds) =>
      nds.map((n) => ({
        ...n,
        data: {
          ...n.data,
          state: stateMap[n.id] ?? 'UNKNOWN',
          uptime: uptimeMap[n.id] ?? 0,
          responseMs: responseMsMap[n.id] ?? 0,
        },
      })),
    );
    setEdges((eds) =>
      eds.map((e) => {
        const depState = stateMap[e.target] ?? 'UNKNOWN';
        const isCritical = depState === 'DEAD' || depState === 'UNHEALTHY';
        const isHealthy = depState === 'HEALTHY';
        const color = isHealthy
          ? '#c8ccdb'
          : (STATE_COLOR[depState as ServiceState] ?? STATE_COLOR.UNKNOWN);
        const dashed = !isHealthy;
        return {
          ...e,
          data: {
            ...((e.data ?? {}) as Record<string, unknown>),
            color,
            dashed,
            strokeWidth: isCritical ? 2 : 1.5,
          },
          markerEnd: {
            type: MarkerType.ArrowClosed,
            color,
            width: 12,
            height: 12,
          },
        };
      }),
    );
  }, [stateMap, uptimeMap, responseMsMap, setNodes, setEdges]);

  const onNodeClick = useCallback((_: React.MouseEvent, node: Node) => {
    if (node.id === IGW_ID) return;
    setSelected((prev) => (prev === node.id ? null : node.id));
  }, []);

  const onNodeDoubleClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      if (node.id === IGW_ID) return;
      navigate(`/services/${encodeURIComponent(node.id)}`);
    },
    [navigate],
  );

  const onPaneClick = useCallback(() => setSelected(null), []);

  if (svcsLoading || cfgLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  const selectedSvc = svcsData?.services.find((s) => s.name === selected);
  const selectedState = selected ? (stateMap[selected] ?? 'UNKNOWN') : null;
  const selectedColor = selectedState ? STATE_COLOR[selectedState] : undefined;

  return (
    <div className="flex h-full overflow-hidden">
      {/* ── Graph canvas ── */}
      <div className="relative flex-1">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={NODE_TYPES}
          edgeTypes={EDGE_TYPES}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onNodeClick={onNodeClick}
          onNodeDoubleClick={onNodeDoubleClick}
          onPaneClick={onPaneClick}
          fitView
          fitViewOptions={{ padding: 0.18 }}
          minZoom={0.3}
          colorMode="light"
          proOptions={{ hideAttribution: true }}
          style={{ background: '#f7f8fc' }}
        >
          <Background
            variant={BackgroundVariant.Dots}
            gap={28}
            size={1}
            color="#dde1ee"
          />
          <Controls
            style={{
              background: '#ffffff',
              border: '1px solid #e2e5ef',
              borderRadius: 10,
              overflow: 'hidden',
            }}
          />
          <MiniMap
            nodeColor={(n) =>
              n.id === IGW_ID
                ? '#eef0f6'
                : (STATE_COLOR[(n.data as ServiceNodeData).state] ?? '#8990aa')
            }
            maskColor="rgba(247,248,252,0.75)"
            style={{
              background: '#ffffff',
              border: '1px solid #e2e5ef',
              borderRadius: 10,
            }}
          />
        </ReactFlow>

        {/* ── Legend overlay ── */}
        <div className="pointer-events-none absolute bottom-4 left-4 flex flex-wrap gap-2">
          {(Object.entries(STATE_COLOR) as [ServiceState, string][]).map(
            ([state, color]) => (
              <span
                key={state}
                className="flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-[10px] font-semibold"
                style={{
                  borderColor: `${color}44`,
                  background: '#ffffff',
                  color,
                }}
              >
                <span
                  className="inline-block h-1.5 w-1.5 rounded-full"
                  style={{ background: color }}
                />
                {STATE_LABEL[state]}
              </span>
            ),
          )}
          {/* Latency bar legend */}
          <span
            className="flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-[10px] font-semibold"
            style={{
              borderColor: '#e2e5ef',
              background: '#ffffff',
              color: '#8990aa',
            }}
          >
            <span
              style={{ display: 'inline-flex', alignItems: 'center', gap: 3 }}
            >
              <span
                style={{
                  width: 14,
                  height: 3,
                  borderRadius: 2,
                  background: '#10b981',
                  display: 'inline-block',
                }}
              />
              {'<'}
              {LATENCY_WARN_MS}ms
            </span>
          </span>
          <span
            className="flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-[10px] font-semibold"
            style={{
              borderColor: '#f59e0b44',
              background: '#ffffff',
              color: '#f59e0b',
            }}
          >
            <span
              style={{ display: 'inline-flex', alignItems: 'center', gap: 3 }}
            >
              <span
                style={{
                  width: 14,
                  height: 3,
                  borderRadius: 2,
                  background: '#f59e0b',
                  display: 'inline-block',
                }}
              />
              ≥{LATENCY_WARN_MS}ms
            </span>
          </span>
        </div>
      </div>

      {/* ── Side panel ── */}
      {selected && selectedSvc && (
        <div
          className="flex w-64 shrink-0 flex-col overflow-y-auto border-l"
          style={{ borderColor: '#e2e5ef', background: '#ffffff' }}
        >
          {/* Header */}
          <div
            className="border-b p-4"
            style={{
              borderColor: `${selectedColor}33`,
              background: `${selectedColor}08`,
            }}
          >
            <p
              className="text-[10px] font-semibold uppercase tracking-widest mb-1"
              style={{ color: '#8990aa' }}
            >
              Service
            </p>
            <h2
              className="text-sm font-bold leading-snug break-all"
              style={{ color: '#1a1f35' }}
            >
              {selectedSvc.name}
            </h2>
            {/* State badge with dot + label */}
            <div className="mt-2 flex items-center gap-1.5">
              <span
                className="inline-block h-2 w-2 rounded-full"
                style={{ background: selectedColor }}
              />
              <span
                className="text-[11px] font-semibold"
                style={{ color: selectedColor }}
              >
                {selectedState ? STATE_LABEL[selectedState] : selectedState}
              </span>
            </div>
          </div>

          {/* Stats */}
          <div className="flex-1 space-y-4 p-4">
            {/* Response time with inline bar */}
            <div>
              <p
                className="mb-1.5 text-[10px] font-semibold uppercase tracking-wider"
                style={{ color: '#8990aa' }}
              >
                Response time
              </p>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <div
                  style={{
                    flex: 1,
                    height: 3,
                    borderRadius: 2,
                    background: '#f0f1f7',
                    overflow: 'hidden',
                  }}
                >
                  <div
                    style={{
                      height: '100%',
                      width: `${Math.min(100, Math.round((selectedSvc.response_time_ms / LATENCY_BAR_MAX_MS) * 100))}%`,
                      background:
                        selectedSvc.response_time_ms >= LATENCY_WARN_MS
                          ? '#f59e0b'
                          : '#10b981',
                      transition: 'width 0.4s ease',
                    }}
                  />
                </div>
                <span
                  style={{
                    fontSize: 11,
                    fontWeight: 600,
                    color:
                      selectedSvc.response_time_ms >= LATENCY_WARN_MS
                        ? '#f59e0b'
                        : '#1a1f35',
                    minWidth: 40,
                    textAlign: 'right',
                  }}
                >
                  {selectedSvc.response_time_ms}ms
                </span>
              </div>
            </div>

            {[
              {
                label: 'Uptime',
                value: `${selectedSvc.uptime_pct.toFixed(2)}%`,
              },
              { label: 'Circuit', value: selectedSvc.circuit.state },
              {
                label: 'Tags',
                value: (selectedSvc.tags ?? []).join(', ') || '—',
              },
            ].map(({ label, value }) => (
              <div key={label}>
                <p
                  className="mb-0.5 text-[10px] font-semibold uppercase tracking-wider"
                  style={{ color: '#8990aa' }}
                >
                  {label}
                </p>
                <p className="text-xs font-medium" style={{ color: '#1a1f35' }}>
                  {value}
                </p>
              </div>
            ))}

            {/* Dependencies */}
            {(
              cfgData?.services.find((s) => s.name === selected)
                ?.dependencies ?? []
            ).length > 0 && (
              <div>
                <p
                  className="mb-1.5 text-[10px] font-semibold uppercase tracking-wider"
                  style={{ color: '#8990aa' }}
                >
                  Depends on
                </p>
                <div className="flex flex-col gap-1">
                  {(
                    cfgData?.services.find((s) => s.name === selected)
                      ?.dependencies ?? []
                  ).map((dep) => {
                    const depState = stateMap[dep] ?? 'UNKNOWN';
                    const dc = STATE_COLOR[depState];
                    const depMs = responseMsMap[dep] ?? 0;
                    return (
                      <button
                        key={dep}
                        onClick={() => setSelected(dep)}
                        className="flex items-center gap-2 rounded-lg px-2.5 py-2 text-left text-xs transition-colors hover:opacity-80"
                        style={{
                          border: `1px solid ${dc}33`,
                          background: `${dc}08`,
                        }}
                      >
                        <span
                          className="h-1.5 w-1.5 rounded-full shrink-0"
                          style={{ background: dc }}
                        />
                        <span
                          className="truncate font-medium flex-1"
                          style={{ color: '#1a1f35' }}
                        >
                          {dep}
                        </span>
                        {/* Latency inline */}
                        {depMs > 0 && (
                          <span
                            className="text-[9px] font-semibold"
                            style={{
                              color:
                                depMs >= LATENCY_WARN_MS
                                  ? '#f59e0b'
                                  : '#8990aa',
                            }}
                          >
                            {depMs}ms
                          </span>
                        )}
                        <span
                          className="text-[9px] font-bold"
                          style={{ color: dc }}
                        >
                          {STATE_LABEL[depState] ?? depState}
                        </span>
                      </button>
                    );
                  })}
                </div>
              </div>
            )}
          </div>

          {/* Footer CTA */}
          <div className="border-t p-4" style={{ borderColor: '#e2e5ef' }}>
            <button
              onClick={() =>
                navigate(`/services/${encodeURIComponent(selectedSvc.name)}`)
              }
              className="w-full rounded-lg py-2 text-xs font-bold transition-opacity hover:opacity-90"
              style={{ background: '#1a1f35', color: '#ffffff' }}
            >
              Open detail →
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
