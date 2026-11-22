import tempfile,unittest
from pathlib import Path
from watchdog import Watchdog
class Tests(unittest.TestCase):
 def test_offline_recovery_dedupe_and_disarm(self):
  with tempfile.TemporaryDirectory() as d:
   sent=[];w=Watchdog(Path(d)/'state.json',lambda *args:sent.append(args) or True)
   key='test_key_1234567890';p={'id':'a'*32,'enabled':True}
   w.heartbeat(key,p,1000);w.check(2500);self.assertEqual(len(sent),0)
   w.check(2501);w.check(2565);self.assertEqual(len(sent),1)
   w=Watchdog(Path(d)/'state.json',lambda *args:sent.append(args) or True)
   w.check(2660);self.assertEqual(len(sent),1)
   w.heartbeat(key,p,2670);w.check(2670);self.assertEqual(len(sent),2)
   w.heartbeat(key,dict(p,enabled=False),2680);w.check(3260);self.assertEqual(len(sent),2)
 def test_failed_push_retry_and_invalid_key(self):
  with tempfile.TemporaryDirectory() as d:
   sent=[];w=Watchdog(Path(d)/'state.json',lambda *a:sent.append(a) and False)
   with self.assertRaises(ValueError):w.heartbeat('../evil',{'id':'a'*32},1000)
   w.heartbeat('test_key_1234567890',{'id':'a'*32},1000);w.check(2501);w.check(2510);self.assertEqual(len(sent),1)
   w.check(2562);self.assertEqual(len(sent),2)
if __name__=='__main__':unittest.main()
