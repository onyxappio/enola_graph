from power_gate import validate
base={'battery_output':"Now drawing from 'AC Power'\n -InternalBattery-0 20%; charging",'custom_output':"Battery Power:\n lowpowermode 1\nAC Power:\n lowpowermode 0\n"}
assert validate(base)['charge_percent']==20
cases=[dict(base,battery_output=base['battery_output'].replace('AC Power','Battery Power')),dict(base,battery_output=base['battery_output'].replace('20%;','19%;')),dict(base,battery_output="Now drawing from 'AC Power'"),dict(base,custom_output=''),dict(base,custom_output='AC Power:\n lowpowermode 1\n'),dict(base,custom_output='AC Power:\n something 0\n')]
for case in cases:
 try:validate(case)
 except ValueError:pass
 else:raise AssertionError(case)
print('PASS: AC 20% accepted; battery, low charge, unknown charge/mode, and AC low power rejected')
