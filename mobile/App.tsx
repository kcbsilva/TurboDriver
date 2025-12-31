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
  Image,
} from 'react-native';
import {darkTheme, lightTheme} from './theme';

const defaultApi = 'http://localhost:8080';
const defaultWs = 'ws://localhost:8080';

type ThemePref = 'system' | 'light' | 'dark';
type WebSocketMessageEvent = {data: string};
type RatingResponse = {
  average: number;
  count: number;
  data: {stars: number; comment?: string; createdAt?: string}[];
};

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
  const [driverID, setDriverID] = useState('mobile_driver');
  const [passengerID, setPassengerID] = useState('mobile_passenger');
  const [rideId, setRideId] = useState('');
  const [log, setLog] = useState<string[]>([]);
  const [ratingStars, setRatingStars] = useState(5);
  const [ratingComment, setRatingComment] = useState('');
  const [profileName, setProfileName] = useState('Taylor Driver');
  const [rideCount, setRideCount] = useState('0');
  const [coverUrl, setCoverUrl] = useState('');
  const [avatarUrl, setAvatarUrl] = useState('');
  const [starFilter, setStarFilter] = useState<number | null>(null);
  const [driverRatings, setDriverRatings] = useState<RatingResponse | null>(
    null,
  );
  const [passengerRatings, setPassengerRatings] =
    useState<RatingResponse | null>(null);
  const [driverSummary, setDriverSummary] = useState<any>(null);
  const [passengerSummary, setPassengerSummary] = useState<any>(null);
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
        `${apiBase}/api/drivers/${driverID}/location`,
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
    if (!locationCode || !licenseNumber || !driverID) {
      logLine('location and license required');
      return;
    }
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

  const fetchRatings = async (role: 'driver' | 'passenger') => {
    const id = role === 'driver' ? driverID : passengerID;
    const token = role === 'driver' ? driverToken : passToken;
    if (!id) {
      logLine(`${role} id required`);
      return;
    }
    if (!token) {
      logLine(`${role} token required`);
      return;
    }
    const path =
      role === 'driver'
        ? `/api/drivers/${id}/ratings`
        : `/api/passengers/${id}/ratings`;
    try {
      const res = await fetch(`${apiBase}${path}`, {
        headers: apiHeaders(token),
      });
      const json = await res.json();
      if (!res.ok) throw new Error(json.error || `status ${res.status}`);
      if (role === 'driver') {
        setDriverRatings(json as RatingResponse);
      } else {
        setPassengerRatings(json as RatingResponse);
      }
      logLine(`fetched ${role} ratings (avg ${(json.average || 0).toFixed(2)})`);
    } catch (err: any) {
      logLine(`ratings fetch failed: ${err.message}`);
    }
  };

  const fetchSummary = async (role: 'driver' | 'passenger') => {
    const id = role === 'driver' ? driverID : passengerID;
    const token = role === 'driver' ? driverToken : passToken;
    if (!id) {
      logLine(`${role} id required`);
      return;
    }
    if (!token) {
      logLine(`${role} token required`);
      return;
    }
    const path =
      role === 'driver'
        ? `/api/drivers/${id}/summary`
        : `/api/passengers/${id}/summary`;
    try {
      const res = await fetch(`${apiBase}${path}`, {headers: apiHeaders(token)});
      const json = await res.json();
      if (!res.ok) throw new Error(json.error || `status ${res.status}`);
      if (role === 'driver') {
        setDriverSummary(json);
        setDriverRatings({
          average: json.ratingAverage || 0,
          count: json.ratingCount || 0,
          data: json.ratings || [],
        });
        if (json.rideCount != null) setRideCount(String(json.rideCount));
      } else {
        setPassengerSummary(json);
        setPassengerRatings({
          average: json.ratingAverage || 0,
          count: json.ratingCount || 0,
          data: json.ratings || [],
        });
        if (json.profile?.fullName) setProfileName(json.profile.fullName);
        if (json.rideCount != null) setRideCount(String(json.rideCount));
      }
      logLine(`fetched ${role} summary`);
    } catch (err: any) {
      logLine(`summary fetch failed: ${err.message}`);
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
          label="Passenger ID"
          value={passengerID}
          onChangeText={setPassengerID}
          styles={styles}
        />
        <LabelInput
          label="Driver ID"
          value={driverID}
          onChangeText={setDriverID}
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
        <View style={styles.actions}>
          <Button title="Get Driver Ratings" onPress={() => fetchRatings('driver')} />
          <Button
            title="Get Passenger Ratings"
            onPress={() => fetchRatings('passenger')}
          />
        </View>
        <View style={styles.actions}>
          <Button title="Driver Summary" onPress={() => fetchSummary('driver')} />
          <Button title="Passenger Summary" onPress={() => fetchSummary('passenger')} />
        </View>
        <Text style={[styles.heading, {marginTop: 16}]}>Profile Preview</Text>
        <LabelInput
          label="Full Name"
          value={profileName}
          onChangeText={setProfileName}
          styles={styles}
        />
        <LabelInput
          label="Ride Count"
          value={rideCount}
          onChangeText={setRideCount}
          styles={styles}
        />
        <LabelInput
          label="Cover Photo URL"
          value={coverUrl}
          onChangeText={setCoverUrl}
          styles={styles}
        />
        <LabelInput
          label="Profile Photo URL"
          value={avatarUrl}
          onChangeText={setAvatarUrl}
          styles={styles}
        />
        <ProfileCard
          name={profileName}
          rides={Number(rideCount) || 0}
          ratings={driverRatings || passengerRatings}
          coverUrl={coverUrl}
          avatarUrl={avatarUrl}
          onFilter={setStarFilter}
          activeFilter={starFilter}
          styles={styles}
        />
        {driverRatings && (
          <RatingBlock
            title="Driver Ratings"
            ratings={driverRatings}
            styles={styles}
            filter={starFilter}
          />
        )}
        {passengerRatings && (
          <RatingBlock
            title="Passenger Ratings"
            ratings={passengerRatings}
            styles={styles}
            filter={starFilter}
          />
        )}
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

function RatingBlock({
  title,
  ratings,
  styles,
  filter,
}: {
  title: string;
  ratings: RatingResponse;
  styles: ReturnType<typeof createStyles>;
  filter?: number | null;
}) {
  const filtered =
    filter == null
      ? ratings.data
      : ratings.data.filter(r => Math.round(r.stars) === filter);
  return (
    <View style={styles.field}>
      <Text style={styles.heading}>{title}</Text>
      <Text style={styles.subhead}>
        Avg: {ratings.average.toFixed(2)} ({ratings.count})
      </Text>
      {filtered.slice(0, 5).map((r, idx) => (
        <Text key={`${title}-${idx}`} style={styles.logLine}>
          {r.stars}★ {r.comment ? `– ${r.comment}` : ''}
        </Text>
      ))}
    </View>
  );
}

function ProfileCard({
  name,
  rides,
  ratings,
  coverUrl,
  avatarUrl,
  onFilter,
  activeFilter,
  styles,
}: {
  name: string;
  rides: number;
  ratings: RatingResponse | null;
  coverUrl: string;
  avatarUrl: string;
  onFilter: (v: number | null) => void;
  activeFilter: number | null;
  styles: ReturnType<typeof createStyles>;
}) {
  const avg = ratings ? ratings.average.toFixed(2) : '0.0';
  const filters = [5, 4, 3, 2, 1];
  return (
    <View style={styles.profileCard}>
      {coverUrl ? (
        <Image source={{uri: coverUrl}} style={styles.cover} />
      ) : (
        <View style={[styles.cover, styles.coverPlaceholder]} />
      )}
      <View style={styles.avatarWrap}>
        {avatarUrl ? (
          <Image source={{uri: avatarUrl}} style={styles.avatar} />
        ) : (
          <View style={[styles.avatar, styles.coverPlaceholder]} />
        )}
      </View>
      <View style={styles.profileBody}>
        <Text style={styles.heading}>{name}</Text>
        <View style={styles.profileStats}>
          <Text style={styles.subhead}>⭐ {avg}</Text>
          <Text style={styles.subhead}>{rides} rides</Text>
        </View>
        <View style={styles.themeButtons}>
          <Pressable
            style={[
              styles.themeButton,
              activeFilter === null && styles.themeButtonActive,
            ]}
            onPress={() => onFilter(null)}>
            <Text
              style={[
                styles.themeButtonText,
                activeFilter === null && styles.themeButtonTextActive,
              ]}>
              All
            </Text>
          </Pressable>
          {filters.map(star => {
            const active = activeFilter === star;
            return (
              <Pressable
                key={star}
                style={[
                  styles.themeButton,
                  active && styles.themeButtonActive,
                ]}
                onPress={() => onFilter(star)}>
                <Text
                  style={[
                    styles.themeButtonText,
                    active && styles.themeButtonTextActive,
                  ]}>
                  {star}★
                </Text>
              </Pressable>
            );
          })}
        </View>
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
    profileCard: {
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: 12,
      overflow: 'hidden',
      backgroundColor: theme.card,
      marginTop: 12,
    },
    cover: {width: '100%', height: 140},
    coverPlaceholder: {backgroundColor: theme.border},
    avatarWrap: {
      position: 'absolute',
      top: 90,
      alignSelf: 'center',
      borderRadius: 48,
      padding: 4,
      backgroundColor: theme.card,
      borderWidth: 2,
      borderColor: theme.border,
    },
    avatar: {width: 88, height: 88, borderRadius: 44},
    profileBody: {paddingTop: 60, paddingHorizontal: 16, paddingBottom: 12},
    profileStats: {
      flexDirection: 'row',
      justifyContent: 'space-between',
      alignItems: 'center',
      marginVertical: 8,
    },
  });

function shuffle(arr: string[]) {
  const a = [...arr];
  for (let i = a.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [a[i], a[j]] = [a[j], a[i]];
  }
  return a;
}
