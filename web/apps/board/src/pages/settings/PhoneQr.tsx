import { encode } from "uqr";

/** 網址的 QR code：自己畫成 SVG（不塞 HTML 字串）。固定白底黑點，深色模式下相機也掃得到。 */
export function PhoneQr({ value, label }: { value: string; label: string }) {
  const { data, size } = encode(value, { border: 2 });
  let path = "";
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      if (data[y]?.[x]) path += `M${x} ${y}h1v1h-1z`;
    }
  }
  return (
    <svg
      data-testid="phone-qr"
      role="img"
      aria-label={label}
      viewBox={`0 0 ${size} ${size}`}
      shapeRendering="crispEdges"
      className="size-[200px] max-w-full rounded-xl bg-white"
    >
      <path d={path} className="fill-black" />
    </svg>
  );
}
