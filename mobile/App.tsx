import React, {useEffect, useMemo, useState} from 'react';
import {
  Button,
  SafeAreaView,
  ScrollView,
  Text,
  TextInput,
  View,
  StyleSheet,
  useColorScheme,
  Pressable,
} from 'react-native';
import {darkTheme, lightTheme} from './theme';

const defaultApi = 'http://localhost:8080';
const defaultWs = 'ws://localhost:8080';

type ThemePref = 'system' | 'light' | 'dark';
type WebSocketMessageEvent = {data: string};

export default function App() {
  const systemScheme = useColorScheme();
  const [themePref, setThemePref] = useState<ThemePref>('system');
  const resolvedScheme =
    themePref === 'system' ? systemScheme ?? 'light' : themePref;
  const theme = resolvedScheme === 'dark' ? darkTheme : lightTheme;
  const styles = useMemo(() => createStyles(theme), [theme]);

  const [apiBase, setApiBase] = useState(defaultApi);
  const [wsBase, setWsBase] = useState(defaultWs);
  const [passToken, setPassToken] = useState('');
  const [driverToken, setDriverToken] = useState('');
  const [rideId, setRideId] = useState('');
  const [log, setLog] = useState<string[]>([]);
  const [ratingStars, setRatingStars] = useState(5);
  const [ratingComment, setRatingComment] = useState('');
  const [locationCode, setLocationCode] = useState('');
  const [licenseNumber, setLicenseNumber] = useState('');
  const [licenseCountry, setLicenseCountry] = useState('');
  const [licenseRegion, setLicenseRegion] = useState('');
  const [licenseExpires, setLicenseExpires] = useState('');
  const [licenseDocUrl, setLicenseDocUrl] = useState('');
  const [remunerated, setRemunerated] = useState(false);
  const [vehicleType, setVehicleType] = useState<'car' | 'motorcycle' | 'bus'>('car');
  const [plateNumber, setPlateNumber] = useState('');
  const [vehicleDocUrl, setVehicleDocUrl] = useState('');
  const [vehicleDocExpires, setVehicleDocExpires] = useState('');
  const [vehicleOwnership, setVehicleOwnership] = useState<'owns' | 'renting' | 'lent'>('owns');
  const [contractUrl, setContractUrl] = useState('');
  const [frontUrl, setFrontUrl] = useState('');
  const [backUrl, setBackUrl] = useState('');
  const [leftUrl, setLeftUrl] = useState('');
  const [rightUrl, setRightUrl] = useState('');
  const [livenessUp, setLivenessUp] = useState('');
  const [livenessDown, setLivenessDown] = useState('');
  const [livenessLeft, setLivenessLeft] = useState('');
  const [livenessRight, setLivenessRight] = useState('');
  const [applicationStatus, setApplicationStatus] = useState('unknown');

  const logLine = (msg: string) =>
    setLog((prev: string[]) => [msg, ...prev].slice(0, 50));

  const apiHeaders = useMemo(
    () => (token: string) => ({
      'Content-Type': 'application/json',
      ...(token ? {Authorization: `Bearer ${token}`} : {}),
    }),
    [],
  );

  const requestRide = async () => {
    try {
      const body = {
        pickupLat: 40.758,
        pickupLong: -73.9855,
        idempotencyKey: `mobile-${Date.now()}`,
      };
      const res = await fetch(`${apiBase}/api/rides`, {
        method: 'POST',
        headers: apiHeaders(passToken),
        body: JSON.stringify(body),
      });
      if (!res.ok) throw new Error(`status ${res.status}`);
      const json = await res.json();
      setRideId(json.id || '');
      logLine(`ride requested: ${json.id}`);
      subscribeWs(json.id);
    } catch (err: any) {
      logLine(`ride request failed: ${err.message}`);
    }
  };

  const acceptRide = async () => {
    if (!rideId) return;
    try {
      const res = await fetch(`${apiBase}/api/rides/${rideId}/accept`, {
        method: 'POST',
        headers: apiHeaders(driverToken),
        body: JSON.stringify({driverId: 'mobile_driver'}),
      });
      if (!res.ok) throw new Error(`status ${res.status}`);
      logLine('ride accepted');
    } catch (err: any) {
      logLine(`accept failed: ${err.message}`);
    }
  };

  const sendHeartbeat = async () => {
    try {
      const res = await fetch(
        `${apiBase}/api/drivers/mobile_driver/location`,
        {
          method: 'POST',
          headers: apiHeaders(driverToken),
          body: JSON.stringify({
            latitude: 40.758,
            longitude: -73.9855,
            accuracy: 5,
            timestamp: Date.now(),
          }),
        },
      );
      if (!res.ok) throw new Error(`status ${res.status}`);
      logLine('heartbeat sent');
    } catch (err: any) {
      logLine(`heartbeat failed: ${err.message}`);
    }
  };

  const subscribeWs = (id: string) => {
    if (!id) return;
    const wsUrl = `${wsBase}/ws/rides/${id}`;
    const socket = new WebSocket(wsUrl);
    socket.onopen = () => logLine('ws connected');
    socket.onmessage = (evt: WebSocketMessageEvent) =>
      logLine(`ws: ${evt.data}`);
    socket.onerror = () => logLine('ws error');
    socket.onclose = () => logLine('ws closed');
  };

  const submitApplication = async () => {
    if (!driverToken) {
      logLine('driver token required');
      return;
    }
    if (!locationCode || !licenseNumber) {
      logLine('location and license required');
      return;
    }
    const driverID = 'mobile_driver';
    const challengeSequence = shuffle(['up', 'down', 'left', 'right']);
    const photos = [
      {angle: 'front', photoUrl: frontUrl},
      {angle: 'back', photoUrl: backUrl},
      {angle: 'left', photoUrl: leftUrl},
      {angle: 'right', photoUrl: rightUrl},
    ];
    const body = {
      locationCode,
      license: {
        number: licenseNumber,
        country: licenseCountry,
        region: licenseRegion,
        expiresAt: licenseExpires,
        remunerated,
        documentUrl: licenseDocUrl,
      },
      vehicle: {
        type: vehicleType,
        plateNumber,
        documentUrl: vehicleDocUrl,
        documentExpiresAt: vehicleDocExpires,
        ownership: vehicleOwnership,
        contractUrl,
      },
      photos,
      liveness: {
        challengeSequence,
        captures: {
          up: livenessUp,
          down: livenessDown,
          left: livenessLeft,
          right: livenessRight,
        },
      },
    };
    try {
      const res = await fetch(`${apiBase}/api/drivers/${driverID}/application`, {
        method: 'POST',
        headers: apiHeaders(driverToken),
        body: JSON.stringify(body),
      });
      const json = await res.json();
      if (!res.ok) throw new Error(json.error || `status ${res.status}`);
      setApplicationStatus(json.status || 'pending');
      logLine('application submitted');
    } catch (err: any) {
      logLine(`application failed: ${err.message}`);
    }
  };

  const fetchApplication = async () => {
    const driverID = 'mobile_driver';
    try {
      const res = await fetch(`${apiBase}/api/drivers/${driverID}/application`, {
        headers: apiHeaders(driverToken),
      });
      const json = await res.json();
      if (!res.ok) throw new Error(json.error || `status ${res.status}`);
      setApplicationStatus(json.status || 'unknown');
      logLine(`application status: ${json.status}`);
    } catch (err: any) {
      logLine(`fetch application failed: ${err.message}`);
    }
  };

  useEffect(() => {
    // auto heartbeat on load to populate driver state
    sendHeartbeat();
  }, []);

  const submitRating = async (role: 'driver' | 'passenger') => {
    if (!rideId) {
      logLine('ride id required to rate');
      return;
    }
    if (ratingStars < 1 || ratingStars > 5) {
      logLine('rating must be 1-5');
      return;
    }
    if (ratingStars <= 3 && !ratingComment.trim()) {
      logLine('comment required for 3 stars or less');
      return;
    }
    const token = role === 'driver' ? driverToken : passToken;
    if (!token) {
      logLine(`${role} token required to rate`);
      return;
    }
    try {
      const res = await fetch(`${apiBase}/api/rides/${rideId}/rating`, {
        method: 'POST',
        headers: apiHeaders(token),
        body: JSON.stringify({stars: ratingStars, comment: ratingComment}),
      });
      const json = await res.json();
      if (!res.ok) throw new Error(json.error || `status ${res.status}`);
      logLine(`rating submitted (${ratingStars} stars)`);
    } catch (err: any) {
      logLine(`rating failed: ${err.message}`);
    }
  };

  return (
    <SafeAreaView style={[styles.screen]}>
      <ScrollView contentContainerStyle={styles.container}>
        <Text style={styles.heading}>TurboDriver Mobile Smoke</Text>
        <ThemeSelector
          pref={themePref}
          onSelect={setThemePref}
          styles={styles}
          systemScheme={systemScheme}
        />
        <LabelInput
          label="API Base"
          value={apiBase}
          onChangeText={setApiBase}
          styles={styles}
        />
        <LabelInput
          label="WS Base"
          value={wsBase}
          onChangeText={setWsBase}
          styles={styles}
        />
        <LabelInput
          label="Passenger Token"
          value={passToken}
          onChangeText={setPassToken}
          styles={styles}
        />
        <LabelInput
          label="Driver Token"
          value={driverToken}
          onChangeText={setDriverToken}
          styles={styles}
        />
        <LabelInput
          label="Ride ID"
          value={rideId}
          onChangeText={setRideId}
          styles={styles}
        />
        <View style={styles.actions}>
          <Button title="Heartbeat" onPress={sendHeartbeat} />
          <Button title="Request Ride" onPress={requestRide} />
          <Button title="Accept Ride" onPress={acceptRide} />
        </View>
        <Text style={styles.subhead}>Log</Text>
        {log.map((l, idx) => (
          <Text key={`${idx}-${l}`} style={styles.logLine}>
            • {l}
          </Text>
        ))}
        <Text style={[styles.heading, {marginTop: 16}]}>Driver Onboarding</Text>
        <LabelInput
          label="Location Code"
          value={locationCode}
          onChangeText={setLocationCode}
          styles={styles}
        />
        <LabelInput
          label="License Number"
          value={licenseNumber}
          onChangeText={setLicenseNumber}
          styles={styles}
        />
        <LabelInput
          label="License Country"
          value={licenseCountry}
          onChangeText={setLicenseCountry}
          styles={styles}
        />
        <LabelInput
          label="License Region"
          value={licenseRegion}
          onChangeText={setLicenseRegion}
          styles={styles}
        />
        <LabelInput
          label="License Expires (RFC3339)"
          value={licenseExpires}
          onChangeText={setLicenseExpires}
          styles={styles}
        />
        <LabelInput
          label="License Document URL"
          value={licenseDocUrl}
          onChangeText={setLicenseDocUrl}
          styles={styles}
        />
        <ToggleRow
          label="Remunerated"
          value={remunerated}
          onToggle={setRemunerated}
          styles={styles}
        />
        <PickerRow
          label="Vehicle Type"
          value={vehicleType}
          options={['car', 'motorcycle', 'bus']}
          onSelect={v => setVehicleType(v as any)}
          styles={styles}
        />
        <PickerRow
          label="Ownership"
          value={vehicleOwnership}
          options={['owns', 'renting', 'lent']}
          onSelect={v => setVehicleOwnership(v as any)}
          styles={styles}
        />
        <LabelInput
          label="Plate Number"
          value={plateNumber}
          onChangeText={setPlateNumber}
          styles={styles}
        />
        <LabelInput
          label="Vehicle Document URL"
          value={vehicleDocUrl}
          onChangeText={setVehicleDocUrl}
          styles={styles}
        />
        <LabelInput
          label="Vehicle Document Expires (RFC3339)"
          value={vehicleDocExpires}
          onChangeText={setVehicleDocExpires}
          styles={styles}
        />
        <LabelInput
          label="Contract URL (if renting/lent)"
          value={contractUrl}
          onChangeText={setContractUrl}
          styles={styles}
        />
        <Text style={[styles.subhead, {marginTop: 12}]}>Vehicle Photos (URLs)</Text>
        <LabelInput label="Front" value={frontUrl} onChangeText={setFrontUrl} styles={styles} />
        <LabelInput label="Back" value={backUrl} onChangeText={setBackUrl} styles={styles} />
        <LabelInput label="Left" value={leftUrl} onChangeText={setLeftUrl} styles={styles} />
        <LabelInput label="Right" value={rightUrl} onChangeText={setRightUrl} styles={styles} />
        <Text style={[styles.subhead, {marginTop: 12}]}>Liveness Captures (URLs)</Text>
        <LabelInput label="Up" value={livenessUp} onChangeText={setLivenessUp} styles={styles} />
        <LabelInput label="Down" value={livenessDown} onChangeText={setLivenessDown} styles={styles} />
        <LabelInput label="Left" value={livenessLeft} onChangeText={setLivenessLeft} styles={styles} />
        <LabelInput label="Right" value={livenessRight} onChangeText={setLivenessRight} styles={styles} />
        <View style={styles.actions}>
          <Button title="Submit Application" onPress={submitApplication} />
          <Button title="Check Status" onPress={fetchApplication} />
        </View>
        <Text style={styles.subhead}>Application Status: {applicationStatus}</Text>
        <Text style={[styles.heading, {marginTop: 16}]}>Rate Ride</Text>
        <SliderRow
          label="Stars"
          value={ratingStars}
          onSelect={setRatingStars}
          styles={styles}
        />
        <LabelInput
          label="Comment (required if ≤3)"
          value={ratingComment}
          onChangeText={setRatingComment}
          styles={styles}
        />
        <View style={styles.actions}>
          <Button title="Passenger Rates" onPress={() => submitRating('passenger')} />
          <Button title="Driver Rates" onPress={() => submitRating('driver')} />
        </View>
      </ScrollView>
    </SafeAreaView>
  );
}

function LabelInput({
  label,
  value,
  onChangeText,
  styles,
}: {
  label: string;
  value: string;
  onChangeText: (v: string) => void;
  styles: ReturnType<typeof createStyles>;
}) {
  return (
    <View style={styles.field}>
      <Text style={styles.label}>{label}</Text>
      <TextInput
        style={styles.input}
        value={value}
        onChangeText={onChangeText}
        autoCapitalize="none"
      />
    </View>
  );
}

function ToggleRow({
  label,
  value,
  onToggle,
  styles,
}: {
  label: string;
  value: boolean;
  onToggle: (v: boolean) => void;
  styles: ReturnType<typeof createStyles>;
}) {
  return (
    <View style={styles.field}>
      <Text style={styles.label}>{label}</Text>
      <View style={styles.themeButtons}>
        <Pressable
          onPress={() => onToggle(true)}
          style={[styles.themeButton, value && styles.themeButtonActive]}>
          <Text
            style={[
              styles.themeButtonText,
              value && styles.themeButtonTextActive,
            ]}>
            Yes
          </Text>
        </Pressable>
        <Pressable
          onPress={() => onToggle(false)}
          style={[styles.themeButton, !value && styles.themeButtonActive]}>
          <Text
            style={[
              styles.themeButtonText,
              !value && styles.themeButtonTextActive,
            ]}>
            No
          </Text>
        </Pressable>
      </View>
    </View>
  );
}

function PickerRow({
  label,
  value,
  options,
  onSelect,
  styles,
}: {
  label: string;
  value: string;
  options: string[];
  onSelect: (v: string) => void;
  styles: ReturnType<typeof createStyles>;
}) {
  return (
    <View style={styles.field}>
      <Text style={styles.label}>{label}</Text>
      <View style={styles.themeButtons}>
        {options.map(opt => {
          const active = opt === value;
          return (
            <Pressable
              key={opt}
              onPress={() => onSelect(opt)}
              style={[styles.themeButton, active && styles.themeButtonActive]}>
              <Text
                style={[
                  styles.themeButtonText,
                  active && styles.themeButtonTextActive,
                ]}>
                {opt}
              </Text>
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}

function SliderRow({
  label,
  value,
  onSelect,
  styles,
}: {
  label: string;
  value: number;
  onSelect: (v: number) => void;
  styles: ReturnType<typeof createStyles>;
}) {
  const options = [1, 2, 3, 4, 5];
  return (
    <View style={styles.field}>
      <Text style={styles.label}>{label}: {value}</Text>
      <View style={styles.themeButtons}>
        {options.map(opt => {
          const active = opt === value;
          return (
            <Pressable
              key={opt}
              onPress={() => onSelect(opt)}
              style={[styles.themeButton, active && styles.themeButtonActive]}>
              <Text
                style={[
                  styles.themeButtonText,
                  active && styles.themeButtonTextActive,
                ]}>
                {opt}
              </Text>
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}

function ThemeSelector({
  pref,
  onSelect,
  styles,
  systemScheme,
}: {
  pref: ThemePref;
  onSelect: (v: ThemePref) => void;
  styles: ReturnType<typeof createStyles>;
  systemScheme: string | null | undefined;
}) {
  const options: Array<{key: ThemePref; label: string}> = [
    {key: 'light', label: 'Light'},
    {key: 'dark', label: 'Dark'},
    {key: 'system', label: 'System'},
  ];
  return (
    <View style={styles.themeRow}>
      <Text style={styles.label}>
        Theme ({pref === 'system' ? `System: ${systemScheme || 'light'}` : 'Manual'}
        )
      </Text>
      <View style={styles.themeButtons}>
        {options.map(opt => {
          const active = opt.key === pref;
          return (
            <Pressable
              key={opt.key}
              onPress={() => onSelect(opt.key)}
              style={[
                styles.themeButton,
                active && styles.themeButtonActive,
              ]}>
              <Text
                style={[
                  styles.themeButtonText,
                  active && styles.themeButtonTextActive,
                ]}>
                {opt.label}
              </Text>
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}

const createStyles = (theme: typeof lightTheme) =>
  StyleSheet.create({
    screen: {flex: 1, backgroundColor: theme.background},
    container: {padding: 16},
    heading: {
      color: theme.textPrimary,
      fontSize: 20,
      marginBottom: 12,
    },
    subhead: {color: theme.textSecondary, marginTop: 16},
    logLine: {color: theme.textPrimary, marginTop: 4},
    actions: {flexDirection: 'row', marginTop: 12, gap: 8},
    field: {marginBottom: 8},
    label: {color: theme.textSecondary, marginBottom: 4},
    input: {
      borderWidth: 1,
      borderColor: theme.border,
      color: theme.textPrimary,
      padding: 8,
      borderRadius: 6,
      backgroundColor: theme.card,
    },
    themeRow: {marginBottom: 12},
    themeButtons: {flexDirection: 'row', gap: 8},
    themeButton: {
      paddingVertical: 8,
      paddingHorizontal: 12,
      borderRadius: 8,
      borderWidth: 1,
      borderColor: theme.border,
      backgroundColor: theme.card,
    },
    themeButtonActive: {
      borderColor: theme.accent,
      backgroundColor: theme.accent + '1a',
    },
    themeButtonText: {color: theme.textSecondary, fontWeight: '500'},
    themeButtonTextActive: {color: theme.accent, fontWeight: '700'},
  });

function shuffle(arr: string[]) {
  const a = [...arr];
  for (let i = a.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [a[i], a[j]] = [a[j], a[i]];
  }
  return a;
}
