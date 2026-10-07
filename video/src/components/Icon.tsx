import type { CSSProperties } from 'react';
// the icon set of the web UI, so the video uses the same pictograms as the product
import { icons } from '../../../web/src/lib/components/ui/icons';

export type IconName = keyof typeof icons;

export const Icon = ({
	name,
	size = 24,
	color = 'currentColor',
	stroke = 2,
	style
}: {
	name: IconName;
	size?: number;
	color?: string;
	stroke?: number;
	style?: CSSProperties;
}) => (
	<svg
		width={size}
		height={size}
		viewBox="0 0 24 24"
		fill="none"
		stroke={color}
		strokeWidth={stroke}
		strokeLinecap="round"
		strokeLinejoin="round"
		style={{ flexShrink: 0, ...style }}
	>
		{icons[name].map((d, i) => (
			<path key={i} d={d} />
		))}
	</svg>
);
